package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type SummaryGenerator interface {
	SummarizeSessionContext(ctx context.Context, contents []string) (string, error)
}

type SummaryWorker struct {
	repo   *Repository
	llm    SummaryGenerator
	logger *slog.Logger
}

type SummaryWorkerOption func(*SummaryWorker)

func WithSummaryWorkerLogger(logger *slog.Logger) SummaryWorkerOption {
	return func(w *SummaryWorker) {
		if logger != nil {
			w.logger = logger
		}
	}
}

func NewSummaryWorker(repo *Repository, generator SummaryGenerator, opts ...SummaryWorkerOption) *SummaryWorker {
	w := &SummaryWorker{
		repo:   repo,
		llm:    generator,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *SummaryWorker) Handle(ctx context.Context, payload []byte) error {
	var job SummaryJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrInvalidSummaryJob, err)
	}

	sessionID, userID, err := job.Validate()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSummaryJob, err)
	}

	total, err := w.repo.CountChats(ctx, sessionID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if total == 0 || total%SummaryBatchSize != 0 {
		return nil
	}

	_, covered, err := w.repo.GetSummary(ctx, sessionID)
	if err != nil && !errors.Is(err, ErrSummaryNotFound) {
		return err
	}
	if covered >= total {
		return nil
	}

	contents, err := w.repo.ListRecentChatContents(ctx, sessionID, summaryRecentSize)
	if err != nil {
		return err
	}

	summary, err := w.llm.SummarizeSessionContext(ctx, contents)
	if err != nil {
		return fmt.Errorf("summarize session: %w", err)
	}

	summary = strings.TrimSpace(summary)
	if summary == "" {
		return errors.New("summarize session: empty summary")
	}

	if err := w.repo.UpsertSummary(ctx, sessionID, summary, total); err != nil {
		return err
	}

	w.logger.Info("session summary stored",
		"session_id", job.SessionID,
		"covered_chats", total,
	)
	return nil
}
