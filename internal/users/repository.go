package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

func (r *Repository) CreateProfile(ctx context.Context, userID uuid.UUID, name string, questions []Question) (database.CreateProfileRow, error) {
	raw, err := json.Marshal(questions)
	if err != nil {
		return database.CreateProfileRow{}, fmt.Errorf("%w: marshal questions: %v", ErrInternal, err)
	}

	profile, err := r.db.CreateProfile(ctx, database.CreateProfileParams{
		Name:      name,
		Questions: raw,
		Userid:    userID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return database.CreateProfileRow{}, ErrAlreadyExists
		}
		return database.CreateProfileRow{}, fmt.Errorf("%w: db CreateProfile: %v", ErrInternal, err)
	}

	return profile, nil
}

func (r *Repository) GetProfileByUserID(ctx context.Context, userID uuid.UUID) (database.GetProfileByUserIDRow, error) {
	profile, err := r.db.GetProfileByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.GetProfileByUserIDRow{}, ErrNotFound
		}
		return database.GetProfileByUserIDRow{}, fmt.Errorf("%w: db GetProfileByUserID: %v", ErrInternal, err)
	}

	return profile, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, userID uuid.UUID, name string, questions []Question) (database.UpdateProfileRow, error) {
	raw, err := json.Marshal(questions)
	if err != nil {
		return database.UpdateProfileRow{}, fmt.Errorf("%w: marshal questions: %v", ErrInternal, err)
	}

	profile, err := r.db.UpdateProfile(ctx, database.UpdateProfileParams{
		Userid:    userID,
		Name:      name,
		Questions: raw,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.UpdateProfileRow{}, ErrNotFound
		}
		return database.UpdateProfileRow{}, fmt.Errorf("%w: db UpdateProfile: %v", ErrInternal, err)
	}

	return profile, nil
}

func (r *Repository) ProfileExists(ctx context.Context, userID uuid.UUID) (bool, error) {
	exists, err := r.db.ProfileExistsByUserID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("%w: db ProfileExistsByUserID: %v", ErrInternal, err)
	}

	return exists, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
		strings.Contains(strings.ToLower(err.Error()), "unique")
}
