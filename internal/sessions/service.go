package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrInternal     = errors.New("something went wrong")
	ErrNotFound     = errors.New("session not found")
)

type Service struct {
	repo *Repository
}

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxPage      = 1_000_000
	MaxLimit     = 100
)

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateSession(ctx context.Context, userIDStr, name string) (database.Session, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Session{}, err
	}

	return s.repo.CreateSession(ctx, userID, name)
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
