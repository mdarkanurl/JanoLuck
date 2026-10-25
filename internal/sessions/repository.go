package sessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

type Repository struct {
	db *database.Queries
}

func NewRepository(dbQueries *database.Queries) *Repository {
	return &Repository{db: dbQueries}
}

func (r *Repository) CreateSession(ctx context.Context, userID uuid.UUID, name string) (database.Session, error) {
	session, err := r.db.CreateSession(ctx, database.CreateSessionParams{
		Name:     name,
		Userid:   userID,
		UpdateAt: time.Now().UTC(),
	})
	if err != nil {
		return database.Session{}, fmt.Errorf("%w: db CreateSession: %v", ErrInternal, err)
	}

	return session, nil
}

func (r *Repository) ListSessionsByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]database.Session, error) {
	sessions, err := r.db.ListSessionsByUserID(ctx, database.ListSessionsByUserIDParams{
		Userid: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: db ListSessionsByUserID: %v", ErrInternal, err)
	}

	if sessions == nil {
		sessions = []database.Session{}
	}

	return sessions, nil
}

func (r *Repository) CountSessionsByUserID(ctx context.Context, userID uuid.UUID) (int, error) {
	total, err := r.db.CountSessionsByUserID(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("%w: db CountSessionsByUserID: %v", ErrInternal, err)
	}

	return int(total), nil
}

func (r *Repository) GetSessionByID(ctx context.Context, sessionID, userID uuid.UUID) (database.Session, error) {
	session, err := r.db.GetSessionByID(ctx, database.GetSessionByIDParams{
		ID:     sessionID,
		Userid: userID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Session{}, ErrNotFound
		}
		return database.Session{}, fmt.Errorf("%w: db GetSessionByID: %v", ErrInternal, err)
	}

	return session, nil
}

func (r *Repository) DeleteSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	deleted, err := r.db.DeleteSession(ctx, database.DeleteSessionParams{
		ID:     sessionID,
		Userid: userID,
	})
	if err != nil {
		return fmt.Errorf("%w: db DeleteSession: %v", ErrInternal, err)
	}

	if deleted == 0 {
		return ErrNotFound
	}

	return nil
}
