package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

const Topic = "moderation.requested"

const (
	KindMode = "mode"
	KindChat = "chat"

	StatusAllowed = "allowed"
	StatusBlocked = "blocked"
)

var ErrNotFound = errors.New("moderation target not found")

type Job struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

func (j Job) Validate() error {
	if j.Kind != KindMode && j.Kind != KindChat {
		return fmt.Errorf("kind must be %q or %q", KindMode, KindChat)
	}
	if _, err := uuid.Parse(strings.TrimSpace(j.ID)); err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}
	if strings.TrimSpace(j.Content) == "" {
		return errors.New("content is required")
	}
	return nil
}

type Moderator interface {
	ModerateContentContext(ctx context.Context, content string) (bool, string, error)
}

type Store interface {
	SetModeStatus(ctx context.Context, id uuid.UUID, status, reason string) error
	SetChatStatus(ctx context.Context, id uuid.UUID, status, reason string) error
}

type Worker struct {
	store     Store
	moderator Moderator
	logger    *slog.Logger
}

func NewWorker(store Store, moderator Moderator, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, moderator: moderator, logger: logger}
}

func (w *Worker) Handle(ctx context.Context, payload []byte) error {
	var job Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("moderation: decode job: %w", err)
	}
	if err := job.Validate(); err != nil {
		return fmt.Errorf("moderation: invalid job: %w", err)
	}
	if w.moderator == nil || w.store == nil {
		return errors.New("moderation: worker is not configured")
	}

	allowed, reason, err := w.moderator.ModerateContentContext(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("moderation: classify %s %s: %w", job.Kind, job.ID, err)
	}

	status := StatusBlocked
	if allowed {
		status = StatusAllowed
		reason = ""
	}

	id, _ := uuid.Parse(job.ID)
	switch job.Kind {
	case KindMode:
		err = w.store.SetModeStatus(ctx, id, status, reason)
	case KindChat:
		err = w.store.SetChatStatus(ctx, id, status, reason)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			w.logger.Warn("moderation target no longer exists", "kind", job.Kind, "id", job.ID)
			return nil
		}
		return err
	}

	w.logger.Info(
		"moderation complete",
		"kind", job.Kind,
		"id", job.ID,
		"status", status,
		"reason", reason,
	)
	return nil
}
