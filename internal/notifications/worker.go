package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/email"
)

type EmailPublisher interface {
	Produce(ctx context.Context, topic, key string, value any) error
}

type Worker struct {
	email  EmailPublisher
	logger *slog.Logger
}

func NewWorker(publisher EmailPublisher, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{email: publisher, logger: logger}
}

func (w *Worker) Handle(ctx context.Context, topic string, payload []byte) error {
	var event auth.UserEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("notifications: decode event: %w", err)
	}
	if event.Email == "" {
		return errors.New("notifications: email is required")
	}

	var subject, body string
	switch topic {
	case auth.UserRegisteredTopic:
		subject = "welcome to JanoLuck"
		body = "your account is ready. ask your first question and let the cards speak."
	case auth.UserPasswordChangedTopic:
		subject = "your password was changed"
		body = "your JanoLuck password was just changed. if this was not you, reset it immediately."
	case auth.UserPasswordResetTopic:
		subject = "your password was reset"
		body = "your JanoLuck password was reset successfully. if this was not you, contact support immediately."
	default:
		return nil
	}

	if w.email == nil {
		return errors.New("notifications: email publisher is not configured")
	}

	job := email.Job{
		To:      event.Email,
		Subject: subject,
		Body:    body,
	}
	if err := w.email.Produce(ctx, email.NotificationTopic, job.To, job); err != nil {
		return fmt.Errorf("notifications: enqueue email: %w", err)
	}

	w.logger.Info("notification queued", "topic", topic, "to", event.Email, "subject", subject)
	return nil
}
