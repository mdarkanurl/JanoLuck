package modes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

type Repository struct {
	db *database.Queries
}

func NewRepository(dbQueries *database.Queries) *Repository {
	return &Repository{db: dbQueries}
}

func (r *Repository) CreateMode(ctx context.Context, userID uuid.UUID, name, prompt string) (database.Mode, error) {
	mode, err := r.db.CreateMode(ctx, database.CreateModeParams{
		Userid: uuid.NullUUID{UUID: userID, Valid: true},
		Name:   name,
		Prompt: prompt,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return database.Mode{}, ErrAlreadyExists
		}
		return database.Mode{}, fmt.Errorf("%w: db CreateMode: %v", ErrInternal, err)
	}

	return mode, nil
}

func (r *Repository) ListModesByUserID(ctx context.Context, userID uuid.UUID) ([]database.Mode, error) {
	modes, err := r.db.ListModesByUserID(ctx, uuid.NullUUID{UUID: userID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("%w: db ListModesByUserID: %v", ErrInternal, err)
	}

	if modes == nil {
		modes = []database.Mode{}
	}

	return modes, nil
}

func (r *Repository) GetModeByID(ctx context.Context, modeID, userID uuid.UUID) (database.Mode, error) {
	mode, err := r.db.GetModeByID(ctx, database.GetModeByIDParams{
		ID:     modeID,
		Userid: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Mode{}, ErrNotFound
		}
		return database.Mode{}, fmt.Errorf("%w: db GetModeByID: %v", ErrInternal, err)
	}

	return mode, nil
}

func (r *Repository) UpdateMode(ctx context.Context, modeID, userID uuid.UUID, name, prompt string) (database.Mode, error) {
	mode, err := r.db.UpdateMode(ctx, database.UpdateModeParams{
		ID:       modeID,
		Userid:   uuid.NullUUID{UUID: userID, Valid: true},
		Name:     name,
		Prompt:   prompt,
		UpdateAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Mode{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return database.Mode{}, ErrAlreadyExists
		}
		return database.Mode{}, fmt.Errorf("%w: db UpdateMode: %v", ErrInternal, err)
	}

	return mode, nil
}

func (r *Repository) DeleteMode(ctx context.Context, modeID, userID uuid.UUID) error {
	deleted, err := r.db.DeleteMode(ctx, database.DeleteModeParams{
		ID:     modeID,
		Userid: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("%w: db DeleteMode: %v", ErrInternal, err)
	}

	if deleted == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) GetActiveModeByUserID(ctx context.Context, userID uuid.UUID) (database.Mode, error) {
	mode, err := r.db.GetActiveModeByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Mode{}, ErrNotFound
		}
		return database.Mode{}, fmt.Errorf("%w: db GetActiveModeByUserID: %v", ErrInternal, err)
	}

	return mode, nil
}

func (r *Repository) SetActiveMode(ctx context.Context, userID, modeID uuid.UUID) (uuid.UUID, error) {
	activeModeID, err := r.db.SetActiveMode(ctx, database.SetActiveModeParams{
		Userid:   userID,
		Modeid:   modeID,
		Updateat: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("%w: db SetActiveMode: %v", ErrInternal, err)
	}

	return activeModeID, nil
}

func (r *Repository) ClearActiveMode(ctx context.Context, userID uuid.UUID) error {
	if _, err := r.db.ClearActiveMode(ctx, userID); err != nil {
		return fmt.Errorf("%w: db ClearActiveMode: %v", ErrInternal, err)
	}

	return nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
		strings.Contains(strings.ToLower(err.Error()), "unique")
}
