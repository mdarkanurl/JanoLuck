package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

const Topic = "analytics.events"

type Event struct {
	Type       string         `json:"type"`
	UserID     string         `json:"user_id,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	ChatID     string         `json:"chat_id,omitempty"`
	ModeID     string         `json:"mode_id,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	Properties map[string]any `json:"properties,omitempty"`
}

type Publisher interface {
	ProduceAsync(ctx context.Context, topic, key string, value any) error
}

func Record(ctx context.Context, publisher Publisher, event Event) {
	if publisher == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	key := event.UserID
	if key == "" {
		key = event.Type
	}
	_ = publisher.ProduceAsync(ctx, Topic, key, event)
}

type Worker struct {
	logger *slog.Logger
}

func NewWorker(logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{logger: logger}
}

func (w *Worker) Handle(_ context.Context, payload []byte) error {
	var event Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.New("analytics: decode event: " + err.Error())
	}
	if event.Type == "" {
		return errors.New("analytics: event type is required")
	}
	w.logger.Info(
		"analytics event",
		"type", event.Type,
		"user_id", event.UserID,
		"session_id", event.SessionID,
		"chat_id", event.ChatID,
		"mode_id", event.ModeID,
		"properties", event.Properties,
	)
	return nil
}
