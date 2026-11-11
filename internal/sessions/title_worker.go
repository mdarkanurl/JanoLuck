package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTitleMaxAge   = 10 * time.Minute
	maxSessionNameLength = 120
)

var ErrInvalidTitleJob = errors.New("invalid session title job")

type TitleGenerator interface {
	GenerateSessionNameContext(ctx context.Context, content string) (string, error)
}

type TitleWorker struct {
	repo   *Repository
	llm    TitleGenerator
	maxAge time.Duration
	logger *slog.Logger
}

type TitleWorkerOption func(*TitleWorker)

func WithTitleMaxAge(maxAge time.Duration) TitleWorkerOption {
	return func(w *TitleWorker) {
		if maxAge > 0 {
			w.maxAge = maxAge
		}
	}
}

func WithTitleWorkerLogger(logger *slog.Logger) TitleWorkerOption {
	return func(w *TitleWorker) {
		if logger != nil {
			w.logger = logger
		}
	}
}

func NewTitleWorker(repo *Repository, generator TitleGenerator, opts ...TitleWorkerOption) *TitleWorker {
	w := &TitleWorker{
		repo:   repo,
		llm:    generator,
		maxAge: defaultTitleMaxAge,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *TitleWorker) Handle(ctx context.Context, payload []byte, publishedAt time.Time) error {
	var job TitleJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrInvalidTitleJob, err)
	}
	if err := job.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTitleJob, err)
	}

	if !publishedAt.IsZero() && w.maxAge > 0 && time.Since(publishedAt) > w.maxAge {
		w.logger.Warn("skipping stale session title job",
			"session_id", job.SessionID,
			"published_at", publishedAt,
			"max_age", w.maxAge,
		)
		return nil
	}

	sessionID, err := uuid.Parse(job.SessionID)
	if err != nil {
		return fmt.Errorf("%w: invalid session id: %v", ErrInvalidTitleJob, err)
	}
	userID, err := uuid.Parse(job.UserID)
	if err != nil {
		return fmt.Errorf("%w: invalid user id: %v", ErrInvalidTitleJob, err)
	}

	name, err := w.llm.GenerateSessionNameContext(ctx, job.Content)
	if err != nil {
		return fmt.Errorf("generate session name: %w", err)
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("generate session name: empty name")
	}
	if len([]rune(name)) > maxSessionNameLength {
		name = string([]rune(name)[:maxSessionNameLength])
	}

	if err := w.repo.UpdateSessionName(ctx, sessionID, userID, name); err != nil {
		if errors.Is(err, ErrNotFound) {
			w.logger.Warn("session no longer exists; skipping title", "session_id", job.SessionID)
			return nil
		}
		return err
	}

	w.logger.Info("session title generated", "session_id", job.SessionID, "name", name)
	return nil
}
