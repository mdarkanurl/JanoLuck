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
cmd/cli/main.go        # CLI entry point
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

```bash
go run ./cmd/cli/main.go
```

## Database Migrations

```bash
goose postgres "..." up
```