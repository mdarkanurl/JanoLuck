package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

const Topic = "chat.embedding.requested"

type Job struct {
	ChatID  string `json:"chat_id"`
	Content string `json:"content"`
}

func (j Job) Validate() error {
	if strings.TrimSpace(j.ChatID) == "" {
		return errors.New("chat id is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(j.ChatID)); err != nil {
		return fmt.Errorf("invalid chat id: %v", err)
	}
	if strings.TrimSpace(j.Content) == "" {
		return errors.New("content is required")
	}
	return nil
}

type Embedder interface {
	GenerateEmbeddingContext(ctx context.Context, content string) ([]float64, error)
}

type Worker struct {
	repo     *Repository
	embedder Embedder
	logger   *slog.Logger
}

func NewWorker(repo *Repository, embedder Embedder, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{repo: repo, embedder: embedder, logger: logger}
}

func (w *Worker) Handle(ctx context.Context, payload []byte) error {
	var job Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("embeddings: decode job: %w", err)
	}
	if err := job.Validate(); err != nil {
		return fmt.Errorf("embeddings: invalid job: %w", err)
	}
	if w.embedder == nil {
		return errors.New("embeddings: embedder is not configured")
	}

	chatID, _ := uuid.Parse(job.ChatID)
	vector, err := w.embedder.GenerateEmbeddingContext(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("embeddings: generate for chat %s: %w", job.ChatID, err)
	}

	if err := w.repo.UpdateEmbedding(ctx, chatID, VectorString(vector)); err != nil {
		if errors.Is(err, ErrNotFound) {
			w.logger.Warn("chat no longer exists; skipping embedding", "chat_id", job.ChatID)
			return nil
		}
		return err
	}

	w.logger.Info("chat embedding stored", "chat_id", job.ChatID)
	return nil
}

func VectorString(vector []float64) string {
	if len(vector) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range vector {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%g", v))
	}
	sb.WriteString("]")
	return sb.String()
}
