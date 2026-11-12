# JanoLuck

A Go-based web application with JWT authentication, email verification, and user profiles.

## Features

- User sign-up, login, and email verification
- JWT access/refresh tokens with Redis blacklisting
- Password reset and change flows
- User profiles with custom questions
- Prediction modes: built-in (Motivation, Islamic) and user-created modes
- Strong password validation
- Email sending via Resend

## Prediction modes

Each user can select one active mode. The mode's instructions are added to the
LLM prompt for every prediction in that user's sessions. Built-in modes
(`Motivation` and `Islamic`) are seeded by the database migration; users can
also create their own.

| Method | Endpoint | Action |
| --- | --- | --- |
| `POST` | `/api/v1/modes` | Create a custom mode |
| `GET` | `/api/v1/modes` | List built-in and own modes |
| `GET` | `/api/v1/modes/{modeID}` | Get a mode |
| `PUT` | `/api/v1/modes/{modeID}` | Update an own mode |
| `DELETE` | `/api/v1/modes/{modeID}` | Delete an own mode |
| `GET` | `/api/v1/modes/active` | Get the active mode |
| `PUT` | `/api/v1/modes/active` | Set the active mode (`{"modeId":"..."}`) |
| `DELETE` | `/api/v1/modes/active` | Clear the active mode |

All mode endpoints require a valid Bearer access token. Built-in modes can be
selected but not edited or deleted. `GET /api/v1/modes` supports `page` and
`limit` query parameters (`page` defaults to `1`, `limit` defaults to `20`, max
`100` per page).

## Streaming predictions

`POST /api/v1/sessions/{sessionID}/chats/stream` stores the message and streams
the prediction back as server-sent events, so the reply can be shown while it is
still being written. `POST /api/v1/sessions/{sessionID}/chats` keeps working
and returns the whole prediction in one response.

Every event is a `data:` line holding a JSON object:

| Type | Payload | Meaning |
| --- | --- | --- |
| `delta` | `{"type":"delta","content":"..."}` | A fragment of the prediction |
| `done` | `{"type":"done","chat":{...}}` | The stored reply |
| `error` | `{"type":"error","error":"..."}` | The prediction failed |

The terminal app uses the streaming endpoint and falls back to the regular one
when the server does not offer it.

## Tech Stack

- Go 1.27
- chi router
- PostgreSQL (with sqlc)
- Redis
- Kafka (confluent-kafka-go)
- JWT (golang-jwt)
- Resend for emails
- validator for request validation

## Project Structure

```
cmd/cli/               # terminal chat app (Bubble Tea)
cmd/worker/            # Kafka worker (email, notifications, audit, analytics, LLM jobs)
cmd/embedding-backfill/ # re-queues chat embeddings for backfill
cmd/kafka-topics/      # creates the Kafka topics used by the app
internal/
  analytics/           # Analytics event schema and consumer
  audit/               # Audit log repository and consumer
  auth/                # Authentication (handlers, service, middleware, tokens)
  chats/               # Chat handlers, service, repository
  embeddings/          # Async chat-embedding worker and repository
  moderation/          # Content moderation worker and repository
  notifications/       # Auth-event to email notification worker
  outbox/              # Transactional outbox repository and relay
  sessions/            # Sessions, async titles and async summaries
  topics/              # Canonical topic list
  users/               # User profiles (handlers, service, repository)
  modes/               # Prediction modes (handlers, service, repository)
  config/              # Environment configuration
  database/            # Generated sqlc code
  email/               # Email service and worker
  kafka/               # Kafka producer, consumer and admin client
sql/
  schema/              # Goose migration files
  queries/             # SQL queries
```

## Configuration

Copy `.env.example` to `.env` and set the required values:

```env
PORT=3000
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/janoluck?sslmode=disable
REDIS_ADDR=localhost:6379
KAFKA_BROKERS=localhost:9092
KAFKA_CLIENT_ID=janoluck
KAFKA_CONSUMER_GROUP=janoluck-email-worker
KAFKA_SESSION_CONSUMER_GROUP=janoluck-session-worker
RESEND_API_KEY=your_api_key
JWT_SECRET=your_secret
```

## Kafka

Start the local single-node broker (KRaft, no ZooKeeper):

```bash
docker compose up -d kafka
```

It listens on `localhost:9092`; topic auto-creation is enabled for local
development. Set `KAFKA_BROKERS` (comma-separated) and `KAFKA_CLIENT_ID` in
`.env` to change the defaults.

The HTTP server creates the producer at startup and shares it between the auth
service (verification/reset emails) and the sessions service (title jobs).
Construct it once and reuse it:

```go
producer, err := kafka.NewProducer(
    kafka.ParseBrokers(cfg.KAFKA_BROKERS),
    kafka.WithClientID(cfg.KAFKA_CLIENT_ID),
)
if err != nil {
    // Log and continue. Construction does not require a reachable broker.
}
defer producer.Close()

err = producer.Produce(ctx, "user.events", userID, map[string]any{
    "type": "signed_up",
})
```

`Produce` marshals the value as JSON, uses the key for partitioning and waits
for the broker acknowledgement (or for the context to finish). If Kafka is
unavailable at startup the app keeps running; delivery failures are returned
from `Produce` and `Close`. Use `ProduceRaw` for pre-encoded payloads and
headers.

Signup verification and password reset emails are published to
`email.verification` and `email.password_reset` as JSON jobs
(`{"to","subject","body"}`). Auth lifecycle events also publish to
`user.registered`, `user.password_changed`, `user.password_reset`,
`auth.login_succeeded` and `auth.logout`; the worker turns the first three into
welcome/security emails on `email.notification`, records every event in the
`audit_log` table, and logs them as analytics. Chat, mode and profile actions
publish to `analytics.events`. The API returns an internal error if a critical
email job cannot be published, so keep the worker running alongside it.

Session titles are generated asynchronously. `POST /api/v1/sessions` inserts the
session immediately with a fallback name derived from the first message and
publishes `session.title.requested` (`{"session_id","user_id","content"}`). The
worker calls the LLM and renames the row, so the request no longer waits for the
model. If Kafka is not configured the API falls back to generating the title
inline; a failed publish keeps the fallback name.

Every tenth chat in a session publishes `session.summary.requested`
(`{"session_id","user_id"}`). The worker summarizes the latest messages and
stores the result in `session_summaries`; the next prediction prepends that
summary to the retrieved chat context so long sessions keep their thread.

User chats are also published to `moderation.requested`
(`{"kind","id","user_id","content"}`) and modes to the same topic with
`kind="mode"`. The worker classifies the content and stores
`moderation_status`/`moderation_reason`; blocked modes disappear from list,
get and active-mode queries, while blocked chats remain visible but flagged.
Chat edits publish `chat.embedding.requested` so the stored vector is refreshed
off the request path. `go run ./cmd/embedding-backfill` re-queues embeddings for
chats that have a `NULL` embedding (`-all` re-embeds everything).

The `outbox` table and `internal/outbox` relay provide an at-least-once path for
events that must be written in the same database transaction as the state
change. Chat, mode and user services write their moderation, embedding and
analytics events through the outbox inside the transaction that stores the row,
so a Kafka outage never loses them. The worker drains unpublished rows every two
seconds, publishes them with their stored headers and marks them published;
rows that fail to publish stay pending for the next tick.

Provision the topics explicitly before starting the workers:

```bash
go run ./cmd/kafka-topics -partitions 3
```

The worker also tries to create them on startup and falls back to broker
auto-creation when the admin client is unavailable.

Consumers retry a failing handler three times with exponential backoff and then
park the message on `<topic>.dlq` (payload preserved, with `x-original-topic`,
`x-original-partition`, `x-original-offset` and `x-error` headers). Offsets are
only committed once the message succeeds or reaches the dead-letter topic. Set
the publisher with `kafka.WithDeadLetterPublisher`; without it failed messages
are logged and dropped. `kafka.WithMaxAttempts` and `kafka.WithRetryBackoff`
tune the retry policy. Every JSON message carries `content_type` and
`schema_version` headers, and `ProduceAsync` reports delivery failures through
the producer logger without blocking the request.

`confluent-kafka-go` uses cgo and bundles librdkafka, so builds need `CGO_ENABLED=1`
and a C toolchain. Dependencies are vendored; run `go mod tidy && go mod vendor`
after changing them.

## Worker

The worker runs one consumer per concern so slow LLM calls never delay email
delivery:

```bash
go run ./cmd/worker
```

| Consumer | Group | Topics |
| --- | --- | --- |
| email | `KAFKA_CONSUMER_GROUP` | `email.verification`, `email.password_reset`, `email.notification` |
| session title | `KAFKA_SESSION_CONSUMER_GROUP` | `session.title.requested` |
| session summary | `KAFKA_SESSION_CONSUMER_GROUP` | `session.summary.requested` |
| notifications | `<KAFKA_CLIENT_ID>-notifications` | auth/user events |
| audit | `<KAFKA_CLIENT_ID>-audit` | auth/user events |
| analytics | `<KAFKA_CLIENT_ID>-analytics` | `analytics.events` |
| embeddings | `<KAFKA_CLIENT_ID>-embedding` | `chat.embedding.requested` |
| moderation | `<KAFKA_CLIENT_ID>-moderation` | `moderation.requested` |

It uses `KAFKA_BROKERS`, `KAFKA_CONSUMER_GROUP` (default
`janoluck-email-worker`), `KAFKA_SESSION_CONSUMER_GROUP` (default
`janoluck-session-worker`), `RESEND_API_KEY`, `DATABASE_URL` and the `LLM_*`
settings. Offsets are committed manually after each message. Email sends are
retried up to 5 times with exponential backoff; malformed jobs are sent to the
dead-letter topic, verification jobs older than 5 minutes and password reset
jobs older than 10 minutes are skipped, and sends that still fail after the
final attempt are dead-lettered. Stale session title requests (older than 10
minutes) and titles for deleted sessions are skipped. The outbox relay runs
alongside the consumers and republishes any row that failed to deliver. If the
database is unreachable at startup, the worker logs a warning and runs only the
email and analytics consumers; restart it once Postgres is back so the other
consumers and the outbox relay start. Stop it with `ctrl+c` or `SIGTERM`;
in-flight work is left uncommitted for redelivery.

## Running

Start the API server (requires the `.env` values above):

```bash
go run ./cmd/server
```

The terminal app is an HTTP client for that server. Run it with `go run`:

```bash
go run ./cmd/cli
```

Or build it once:

```bash
go build -o bin/janoluck ./cmd/cli
./bin/janoluck
```

## Terminal app

Running `janoluck` opens a full-screen chat app. If you are not signed in, it
starts on the login screen; `ctrl+n` switches to sign-up and a 6-digit code
from your email verifies the account.

On launch you get a clean, centered composer. Your first message starts a new
conversation automatically; there is no sidebar.

Type `/` in the composer to see matching commands; `tab` completes the
highlighted one. `/session` works as an alias for `/sessions`.

| Command | Action |
| --- | --- |
| `/sessions` | Browse previous conversations (type to filter) |
| `/modes` | Choose the active prediction mode, or manage custom modes |
| `/new` | Start a new conversation |
| `/profile` | Edit your profile |
| `/logout` | Log out |
| `/help` | Show commands and shortcuts |
| `/quit` | Quit |

In the conversation view, your messages align to the right and Jano's to the
left, with a scrollbar down the side:

| Key | Action |
| --- | --- |
| `enter` | Send a message |
| `ctrl+j` | Insert a newline in the composer |
| `↑` / `↓` | Scroll the conversation line by line |
| mouse wheel | Scroll the conversation |
| `pgup` / `pgdown` | Scroll a page |
| `ctrl+u` / `ctrl+d` | Scroll half a page |
| `ctrl+h` | Show commands and shortcuts |
| `ctrl+c` | Quit |

Inside the `/session` picker:

| Key | Action |
| --- | --- |
| type | Filter conversations by name |
| `↑` / `↓` | Move through conversations |
| `enter` | Open the selected conversation |
| `ctrl+d` | Delete the selected conversation |
| `esc` | Close the picker |

Inside the `/modes` picker:

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move through modes |
| `enter` | Make the selected mode active |
| `n` | Create a custom mode |
| `e` | Edit the selected custom mode |
| `d` | Delete the selected custom mode |
| `/` or `tab` | Focus the filter (type to filter by name) |
| `esc` | Leave the filter, or close the picker |

Built-in modes can be selected but not edited or deleted. Custom modes open a
small form: `tab` moves between the name and instructions, `ctrl+j` inserts a
newline in the instructions, `enter` saves and `esc` cancels.

JWTs and the last opened session are stored in the OS config directory
(`~/.config/janoluck/auth.json` on Linux) with `0600` permissions. Override the
location with `JANOLUCK_CONFIG`. Point the app at another API with `--api-url`
or `JANOLUCK_API_URL` (default: `http://localhost:3000`):

```bash
./bin/janoluck --api-url http://localhost:8080
```

## Database Migrations

```bash
goose postgres "..." up
```