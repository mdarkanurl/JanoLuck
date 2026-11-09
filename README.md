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
- JWT (golang-jwt)
- Resend for emails
- validator for request validation

## Project Structure

```
cmd/cli/               # terminal chat app (Bubble Tea)
internal/
  auth/                # Authentication (handlers, service, middleware, tokens)
  users/               # User profiles (handlers, service, repository)
  modes/               # Prediction modes (handlers, service, repository)
  config/              # Environment configuration
  database/            # Generated sqlc code
  email/               # Email service
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
RESEND_API_KEY=your_api_key
JWT_SECRET=your_secret
```

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