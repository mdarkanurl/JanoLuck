package modes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/analytics"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/moderation"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInternal      = errors.New("something went wrong")
	ErrNotFound      = errors.New("mode not found")
	ErrAlreadyExists = errors.New("mode already exists")
	ErrForbidden     = errors.New("built-in modes cannot be modified")
)

type Publisher interface {
	Produce(ctx context.Context, topic, key string, value any) error
	ProduceAsync(ctx context.Context, topic, key string, value any) error
	ProduceRawAsync(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
}

type Service struct {
	repo     *Repository
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

	publishTimeout = 5 * time.Second
)

type ListResult struct {
	Modes        []database.Mode
	Total        int
	ActiveModeID uuid.UUID
}

func NewService(repo *Repository, opts ...ServiceOption) *Service {
	s := &Service{
		repo:   repo,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) CreateMode(ctx context.Context, userIDStr, name, prompt string) (database.Mode, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Mode{}, err
	}

	build := func(mode database.Mode) []outbox.Event {
		return []outbox.Event{
			{
				Topic: moderation.Topic,
				Key:   mode.ID.String(),
				Payload: moderation.Job{
					Kind:    moderation.KindMode,
					ID:      mode.ID.String(),
					UserID:  userID.String(),
					Content: strings.TrimSpace(name + "\n" + prompt),
				},
			},
			{
				Topic: analytics.Topic,
				Key:   userID.String(),
				Payload: analytics.Event{
					Type:   "mode.created",
					UserID: userID.String(),
					ModeID: mode.ID.String(),
				},
			},
		}
	}

	if s.store != nil {
		return s.repo.CreateModeWithEvents(ctx, userID, name, prompt, build)
	}

	mode, err := s.repo.CreateMode(ctx, userID, name, prompt)
	if err != nil {
		return database.Mode{}, err
	}

	for _, event := range build(mode) {
		s.emit(ctx, event)
	}

	return mode, nil
}

func (s *Service) ListModes(ctx context.Context, userIDStr string, page, limit int) (ListResult, error) {
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

	total, err := s.repo.CountModesByUserID(ctx, userID)
	if err != nil {
		return ListResult{}, err
	}

	modes, err := s.repo.ListModesByUserID(ctx, userID, limit, (page-1)*limit)
	if err != nil {
		return ListResult{}, err
	}

	var activeModeID uuid.UUID
	activeMode, err := s.repo.GetActiveModeByUserID(ctx, userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ListResult{Modes: modes, Total: total, ActiveModeID: activeModeID}, err
	}
	if err == nil {
		activeModeID = activeMode.ID
	}

	return ListResult{Modes: modes, Total: total, ActiveModeID: activeModeID}, nil
}

func (s *Service) GetMode(ctx context.Context, userIDStr, modeIDStr string) (database.Mode, error) {
	userID, modeID, err := parseIDs(userIDStr, modeIDStr, "mode id")
	if err != nil {
		return database.Mode{}, err
	}

	return s.repo.GetModeByID(ctx, modeID, userID)
}

func (s *Service) UpdateMode(ctx context.Context, userIDStr, modeIDStr, name, prompt string) (database.Mode, error) {
	userID, modeID, err := parseIDs(userIDStr, modeIDStr, "mode id")
	if err != nil {
		return database.Mode{}, err
	}

	existing, err := s.repo.GetModeByID(ctx, modeID, userID)
	if err != nil {
		return database.Mode{}, err
	}
	if existing.Isbuiltin {
		return database.Mode{}, ErrForbidden
	}

	build := func(mode database.Mode) []outbox.Event {
		return []outbox.Event{
			{
				Topic: moderation.Topic,
				Key:   modeID.String(),
				Payload: moderation.Job{
					Kind:    moderation.KindMode,
					ID:      modeID.String(),
					UserID:  userID.String(),
					Content: strings.TrimSpace(name + "\n" + prompt),
				},
			},
			{
				Topic: analytics.Topic,
				Key:   userID.String(),
				Payload: analytics.Event{
					Type:   "mode.updated",
					UserID: userID.String(),
					ModeID: modeID.String(),
				},
			},
		}
	}

	if s.store != nil {
		return s.repo.UpdateModeWithEvents(ctx, modeID, userID, name, prompt, build)
	}

	updated, err := s.repo.UpdateMode(ctx, modeID, userID, name, prompt)
	if err != nil {
		return database.Mode{}, err
	}

	for _, event := range build(updated) {
		s.emit(ctx, event)
	}

	return updated, nil
}

func (s *Service) DeleteMode(ctx context.Context, userIDStr, modeIDStr string) error {
	userID, modeID, err := parseIDs(userIDStr, modeIDStr, "mode id")
	if err != nil {
		return err
	}

	existing, err := s.repo.GetModeByID(ctx, modeID, userID)
	if err != nil {
		return err
	}
	if existing.Isbuiltin {
		return ErrForbidden
	}

	return s.repo.DeleteMode(ctx, modeID, userID)
}

func (s *Service) GetActiveMode(ctx context.Context, userIDStr string) (database.Mode, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Mode{}, err
	}

	return s.repo.GetActiveModeByUserID(ctx, userID)
}

func (s *Service) SetActiveMode(ctx context.Context, userIDStr, modeIDStr string) (uuid.UUID, error) {
	userID, modeID, err := parseIDs(userIDStr, modeIDStr, "mode id")
	if err != nil {
		return uuid.Nil, err
	}

	return s.repo.SetActiveMode(ctx, userID, modeID)
}

func (s *Service) ClearActiveMode(ctx context.Context, userIDStr string) error {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return err
	}

	return s.repo.ClearActiveMode(ctx, userID)
}

func (s *Service) emit(ctx context.Context, event outbox.Event) {
	if s.store != nil {
		if err := s.store.Enqueue(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
			s.logger.Warn("could not enqueue mode event", "topic", event.Topic, "key", event.Key, "error", err)
		}
		return
	}

	s.publishAsync(ctx, event.Topic, event.Key, event.Payload, event.Headers)
}

func (s *Service) publishAsync(ctx context.Context, topic, key string, value any, headers map[string]string) {
	if s.producer == nil {
		return
	}

	payload, err := json.Marshal(value)
	if err != nil {
		s.logger.Warn("could not encode mode event", "topic", topic, "key", key, "error", err)
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()

	if err := s.producer.ProduceRawAsync(publishCtx, topic, []byte(key), payload, headers); err != nil {
		s.logger.Warn("could not publish mode event", "topic", topic, "key", key, "error", err)
	}
}

func parseIDs(userIDStr, modeIDStr, field string) (uuid.UUID, uuid.UUID, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	modeID, err := parseUUID(modeIDStr, field)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return userID, modeID, nil
}

func parseUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s", ErrInvalidInput, field)
	}

	return id, nil
}
