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
cmd/worker/            # Kafka email worker
internal/
  auth/                # Authentication (handlers, service, middleware, tokens)
  users/               # User profiles (handlers, service, repository)
  modes/               # Prediction modes (handlers, service, repository)
  config/              # Environment configuration
  database/            # Generated sqlc code
  email/               # Email service and worker
  kafka/               # Kafka producer and consumer
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

The HTTP server creates the producer at startup and passes it to the auth
service. Construct it once and reuse it:

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
(`{"to","subject","body"}`). The `cmd/worker` process consumes both topics and
delivers the emails. The API returns an internal error if a job cannot be
published, so keep the worker running alongside it.

`confluent-kafka-go` uses cgo and bundles librdkafka, so builds need `CGO_ENABLED=1`
and a C toolchain. Dependencies are vendored; run `go mod tidy && go mod vendor`
after changing them.

## Email worker

The worker consumes verification and password reset email jobs and sends them
through Resend:

```bash
go run ./cmd/worker
```

It uses `KAFKA_BROKERS`, `KAFKA_CONSUMER_GROUP` (default
`janoluck-email-worker`) and `RESEND_API_KEY`. Offsets are committed manually
after each message. Sends are retried up to 5 times with exponential backoff;
malformed jobs are skipped, verification jobs older than 5 minutes and password
reset jobs older than 10 minutes are skipped, and sends that still fail after
the final attempt are logged and skipped. Stop it with `ctrl+c` or `SIGTERM`;
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