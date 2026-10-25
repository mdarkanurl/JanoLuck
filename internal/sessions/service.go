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

func (s *Service) ListSessions(ctx context.Context, userIDStr string) ([]database.Session, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return nil, err
	}

	return s.repo.ListSessionsByUserID(ctx, userID)
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
