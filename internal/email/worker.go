package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
)

const (
	defaultMaxAttempts         = 5
	defaultRetryDelay          = time.Second
	maxRetryDelay              = 30 * time.Second
	defaultMaxAge              = 5 * time.Minute
	defaultPasswordResetMaxAge = 10 * time.Minute
)

var ErrInvalidJob = errors.New("invalid email job")

type Sender interface {
	Send(to, subject, body string) error
}

type Worker struct {
	sender              Sender
	maxAttempts         int
	retryDelay          time.Duration
	maxAge              time.Duration
	passwordResetMaxAge time.Duration
	logger              *slog.Logger
}

type WorkerOption func(*Worker)

func WithMaxAttempts(attempts int) WorkerOption {
	return func(w *Worker) {
		if attempts > 0 {
			w.maxAttempts = attempts
		}
	}
}

func WithRetryDelay(delay time.Duration) WorkerOption {
	return func(w *Worker) {
		if delay > 0 {
			w.retryDelay = delay
		}
	}
}

func WithMaxAge(maxAge time.Duration) WorkerOption {
	return func(w *Worker) {
		w.maxAge = maxAge
	}
}

func WithPasswordResetMaxAge(maxAge time.Duration) WorkerOption {
	return func(w *Worker) {
		w.passwordResetMaxAge = maxAge
	}
}

func WithWorkerLogger(logger *slog.Logger) WorkerOption {
	return func(w *Worker) {
		if logger != nil {
			w.logger = logger
		}
	}
}

func NewWorker(sender Sender, opts ...WorkerOption) *Worker {
	w := &Worker{
		sender:              sender,
		maxAttempts:         defaultMaxAttempts,
		retryDelay:          defaultRetryDelay,
		maxAge:              defaultMaxAge,
		passwordResetMaxAge: defaultPasswordResetMaxAge,
		logger:              slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *Worker) Handle(ctx context.Context, topic string, payload []byte, publishedAt time.Time) error {
	var job Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrInvalidJob, err)
	}
	if err := job.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJob, err)
	}

	maxAge := w.maxAgeFor(topic)
	if !publishedAt.IsZero() && maxAge > 0 && time.Since(publishedAt) > maxAge {
		w.logger.Warn("skipping stale email job",
			"topic", topic,
			"to", job.To,
			"subject", job.Subject,
			"published_at", publishedAt,
			"max_age", maxAge,
		)
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= w.maxAttempts; attempt++ {
		if err := w.sender.Send(job.To, job.Subject, job.Body); err != nil {
			lastErr = err
			if attempt == w.maxAttempts {
				break
			}

			delay := w.retryDelayFor(attempt)
			w.logger.Warn("send email failed; retrying",
				"to", job.To,
				"attempt", attempt,
				"max_attempts", w.maxAttempts,
				"retry_in", delay,
				"error", err,
			)

			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}

		w.logger.Info("sent email", "to", job.To, "subject", job.Subject)
		return nil
	}

	return fmt.Errorf("send email to %q after %d attempts: %w", job.To, w.maxAttempts, lastErr)
}

func (w *Worker) maxAgeFor(topic string) time.Duration {
	if topic == PasswordResetTopic {
		return w.passwordResetMaxAge
	}
	return w.maxAge
}

func (w *Worker) retryDelayFor(attempt int) time.Duration {
	delay := w.retryDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

func (j Job) Validate() error {
	if strings.TrimSpace(j.To) == "" {
		return errors.New("to is required")
	}
	if _, err := mail.ParseAddress(j.To); err != nil {
		return fmt.Errorf("invalid to address: %v", err)
	}
	if strings.TrimSpace(j.Subject) == "" {
		return errors.New("subject is required")
	}
	if strings.TrimSpace(j.Body) == "" {
		return errors.New("body is required")
	}
	return nil
}
