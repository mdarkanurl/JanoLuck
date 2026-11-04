# JanoLuck

A Go-based web application with JWT authentication, email verification, and user profiles.

## Features

- User sign-up, login, and email verification
- JWT access/refresh tokens with Redis blacklisting
- Password reset and change flows
- User profiles with custom questions
- Strong password validation
- Email sending via Resend

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

The workspace has a sessions sidebar, the conversation, and a composer. Your
first message starts a new conversation automatically.

| Key | Action |
| --- | --- |
| `enter` | Send a message / open the selected session |
| `ctrl+j` | Insert a newline in the composer |
| `tab` | Move focus between the sidebar and the composer |
| `j` / `k` | Move through sessions when the sidebar is focused |
| `ctrl+n` | Start a new conversation |
| `ctrl+r` | Refresh sessions and messages (or `r` in the sidebar) |
| `d` | Delete the selected session |
| `ctrl+p` | Edit your profile |
| `ctrl+l` | Log out |
| `ctrl+h` | Show keyboard shortcuts |
| `ctrl+c` | Quit |

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