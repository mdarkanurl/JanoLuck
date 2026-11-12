package sessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
)

var (
	ErrInvalidInput      = errors.New("invalid input")
	ErrInternal          = errors.New("something went wrong")
	ErrNotFound          = errors.New("session not found")
	ErrSummaryNotFound   = errors.New("session summary not found")
	ErrInvalidSummaryJob = errors.New("invalid session summary job")
)

type Publisher interface {
	Produce(ctx context.Context, topic, key string, value any) error
}

type Service struct {
	repo     *Repository
	titles   TitleGenerator
	producer Publisher
	store    *outbox.Repository
	logger   *slog.Logger
}

type ServiceOption func(*Service)

func WithProducer(producer Publisher) ServiceOption {
	return func(s *Service) {
		if producer != nil {
			s.producer = producer
		}
	}
}

func WithEventStore(store *outbox.Repository) ServiceOption {
	return func(s *Service) {
		if store != nil {
			s.store = store
		}
	}
}

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		if logger != nil {
			s.logger = logger
		}
	}
}

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxPage      = 1_000_000
	MaxLimit     = 100

	titlePublishTimeout = 5 * time.Second
)

func NewService(repo *Repository, generator TitleGenerator, opts ...ServiceOption) *Service {
	s := &Service{
		repo:   repo,
		titles: generator,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) CreateSession(ctx context.Context, userIDStr, content string) (database.Session, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Session{}, err
	}

	name := fallbackSessionName(content)

	if s.producer == nil && s.store == nil {
		if s.titles != nil {
			generated, genErr := s.titles.GenerateSessionNameContext(ctx, content)
			if genErr != nil {
				s.logger.Warn("could not generate session name synchronously", "error", genErr)
			} else if trimmed := strings.TrimSpace(generated); trimmed != "" {
				name = trimmed
			}
		}
	}

	session, err := s.repo.CreateSession(ctx, userID, name)
	if err != nil {
		return database.Session{}, err
	}

	s.enqueueTitle(ctx, session, content)

	return session, nil
}

func (s *Service) enqueueTitle(ctx context.Context, session database.Session, content string) {
	sessionID := session.ID.String()
	job := TitleJob{
		SessionID: sessionID,
		UserID:    session.Userid.String(),
		Content:   content,
	}

	if s.store != nil {
		if err := s.store.Enqueue(ctx, TitleTopic, sessionID, job, nil); err != nil {
			s.logger.Warn(
				"could not enqueue session title job; keeping fallback name",
				"session_id", sessionID,
				"error", err,
			)
		}
		return
	}

	if s.producer == nil {
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), titlePublishTimeout)
	defer cancel()

	if err := s.producer.Produce(publishCtx, TitleTopic, sessionID, job); err != nil {
		s.logger.Warn(
			"could not enqueue session title job; keeping fallback name",
			"session_id", sessionID,
			"error", err,
		)
	}
}

func fallbackSessionName(content string) string {
	const maxRunes = 60

	name := strings.Join(strings.Fields(content), " ")
	if name == "" {
		return "New session"
	}

	runes := []rune(name)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return name
}

func (s *Service) ListSessions(ctx context.Context, userIDStr string, page, limit int) (ListResult, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return ListResult{}, err
	}

	if page < 1 || page > MaxPage {
		return ListResult{}, fmt.Errorf("%w: page must be between 1 and %d", ErrInvalidInput, MaxPage)
	}
	if limit < 1 || limit > MaxLimit {
		return ListResult{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, MaxLimit)
	}

	total, err := s.repo.CountSessionsByUserID(ctx, userID)
	if err != nil {
		return ListResult{}, err
	}

	sessions, err := s.repo.ListSessionsByUserID(ctx, userID, limit, (page-1)*limit)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{Sessions: sessions, Total: total}, nil
}

func (s *Service) GetSession(ctx context.Context, userIDStr, sessionIDStr string) (database.Session, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Session{}, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return database.Session{}, err
	}

	return s.repo.GetSessionByID(ctx, sessionID, userID)
}

func (s *Service) DeleteSession(ctx context.Context, userIDStr, sessionIDStr string) error {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return err
	}

	return s.repo.DeleteSession(ctx, sessionID, userID)
}

func parseUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s", ErrInvalidInput, field)
	}

	return id, nil
}
