package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
)

type Recorder interface {
	Record(ctx context.Context, eventType string, event auth.UserEvent) error
}

type Worker struct {
	recorder Recorder
	logger   *slog.Logger
}

func NewWorker(recorder Recorder, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{recorder: recorder, logger: logger}
}

func (w *Worker) Handle(ctx context.Context, topic string, payload []byte) error {
	var event auth.UserEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("audit: decode event: %w", err)
	}
	if event.UserID == "" {
		return fmt.Errorf("audit: user id is required for %q", topic)
	}
	if w.recorder == nil {
		return fmt.Errorf("audit: recorder is not configured")
	}

	if err := w.recorder.Record(ctx, topic, event); err != nil {
		return err
	}

	w.logger.Info("audit event recorded", "event_type", topic, "user_id", event.UserID)
	return nil
}
