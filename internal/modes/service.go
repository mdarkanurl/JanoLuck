package modes

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInternal      = errors.New("something went wrong")
	ErrNotFound      = errors.New("mode not found")
	ErrAlreadyExists = errors.New("mode already exists")
	ErrForbidden     = errors.New("built-in modes cannot be modified")
)

type Service struct {
	repo *Repository
}

type ListResult struct {
	Modes        []database.Mode
	Total        int
	ActiveModeID uuid.UUID
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateMode(ctx context.Context, userIDStr, name, prompt string) (database.Mode, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return database.Mode{}, err
	}

	// TODO validate the user's given mode by the LLM

	return s.repo.CreateMode(ctx, userID, name, prompt)
}

func (s *Service) ListModes(ctx context.Context, userIDStr string) (ListResult, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return ListResult{}, err
	}

	modes, err := s.repo.ListModesByUserID(ctx, userID)
	if err != nil {
		return ListResult{}, err
	}

	var activeModeID uuid.UUID
	activeMode, err := s.repo.GetActiveModeByUserID(ctx, userID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ListResult{Modes: modes, Total: len(modes), ActiveModeID: activeModeID}, err
	}
	if err == nil {
		activeModeID = activeMode.ID
	}

	return ListResult{Modes: modes, Total: len(modes), ActiveModeID: activeModeID}, nil
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

	return s.repo.UpdateMode(ctx, modeID, userID, name, prompt)
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
