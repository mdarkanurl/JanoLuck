package users

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
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInternal      = errors.New("something went wrong")
	ErrNotFound      = errors.New("profile not found")
	ErrAlreadyExists = errors.New("profile already exists")
)

type Publisher interface {
	ProduceAsync(ctx context.Context, topic, key string, value any) error
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

const publishTimeout = 5 * time.Second

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

func (s *Service) CreateProfile(ctx context.Context, userIDStr, name, qOne, qTwo string) (database.CreateProfileRow, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil || userID == uuid.Nil {
		return database.CreateProfileRow{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}

	if _, err := s.repo.GetProfileByUserID(ctx, userID); err == nil {
		return database.CreateProfileRow{}, ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return database.CreateProfileRow{}, err
	}

	questions := BuildFixedQuestions(qOne, qTwo)

	profile, err := s.repo.CreateProfile(ctx, userID, name, questions)
	if err != nil {
		return database.CreateProfileRow{}, err
	}

	s.recordAnalytics(ctx, analytics.Event{
		Type:   "profile.created",
		UserID: userID.String(),
	})

	return profile, nil
}

func (s *Service) GetProfile(ctx context.Context, userIDStr string) (database.GetProfileByUserIDRow, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil || userID == uuid.Nil {
		return database.GetProfileByUserIDRow{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}

	return s.repo.GetProfileByUserID(ctx, userID)
}

func (s *Service) ProfileExists(ctx context.Context, userIDStr string) (bool, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil || userID == uuid.Nil {
		return false, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}

	return s.repo.ProfileExists(ctx, userID)
}

func (s *Service) UpdateProfile(ctx context.Context, userIDStr string, req UpdateProfileRequest) (database.UpdateProfileRow, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil || userID == uuid.Nil {
		return database.UpdateProfileRow{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}

	hasName := req.Name != nil && strings.TrimSpace(*req.Name) != ""
	hasQuestionUpdates := len(req.Questions) > 0

	if !hasName && !hasQuestionUpdates {
		return database.UpdateProfileRow{}, fmt.Errorf("%w: provide name or questions to update", ErrInvalidInput)
	}

	existing, err := s.repo.GetProfileByUserID(ctx, userID)
	if err != nil {
		return database.UpdateProfileRow{}, err
	}

	var questions []Question
	if err := json.Unmarshal(existing.Questions, &questions); err != nil {
		return database.UpdateProfileRow{}, fmt.Errorf("%w: corrupt profile questions: %v", ErrInternal, err)
	}

	name := existing.Name
	if hasName {
		name = strings.TrimSpace(*req.Name)
	}

	if hasQuestionUpdates {
		seen := make(map[int]struct{}, len(req.Questions))
		for _, u := range req.Questions {
			targetIndex, ok := u.QuestionIndex()
			if !ok {
				return database.UpdateProfileRow{}, fmt.Errorf("%w: index/id is required for each question", ErrInvalidInput)
			}
			if u.Index != nil && u.ID != nil && *u.Index != *u.ID {
				return database.UpdateProfileRow{}, fmt.Errorf("%w: index and id must match", ErrInvalidInput)
			}
			if u.Answer == nil || strings.TrimSpace(*u.Answer) == "" {
				return database.UpdateProfileRow{}, fmt.Errorf("%w: answer is required for each question", ErrInvalidInput)
			}

			idx := *targetIndex
			if idx < 0 || idx >= len(questions) {
				return database.UpdateProfileRow{}, fmt.Errorf("%w: invalid question index", ErrInvalidInput)
			}
			if _, dup := seen[idx]; dup {
				return database.UpdateProfileRow{}, fmt.Errorf("%w: duplicate question index", ErrInvalidInput)
			}
			seen[idx] = struct{}{}

			questions[idx].Answer = strings.TrimSpace(*u.Answer)
		}
	}

	updated, err := s.repo.UpdateProfile(ctx, userID, name, questions)
	if err != nil {
		return database.UpdateProfileRow{}, err
	}

	s.recordAnalytics(ctx, analytics.Event{
		Type:   "profile.updated",
		UserID: userID.String(),
	})

	return updated, nil
}

func (s *Service) recordAnalytics(ctx context.Context, event analytics.Event) {
	if s.store != nil {
		key := event.UserID
		if key == "" {
			key = event.Type
		}
		if err := s.store.Enqueue(ctx, analytics.Topic, key, event, nil); err != nil {
			s.logger.Warn("could not enqueue analytics event", "type", event.Type, "error", err)
		}
		return
	}

	if s.producer == nil {
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()

	analytics.Record(publishCtx, s.producer, event)
}
