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

func (r *Repository) UpdateSessionName(ctx context.Context, sessionID, userID uuid.UUID, name string) error {
	updated, err := r.db.UpdateSessionName(ctx, database.UpdateSessionNameParams{
		ID:       sessionID,
		Userid:   userID,
		Name:     name,
		UpdateAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("%w: db UpdateSessionName: %v", ErrInternal, err)
	}

	if updated == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) CountChats(ctx context.Context, sessionID, userID uuid.UUID) (int, error) {
	total, err := r.db.CountChatsBySessionID(ctx, database.CountChatsBySessionIDParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Userid:    uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("%w: db CountChatsBySessionID: %v", ErrInternal, err)
	}

	return int(total), nil
}

func (r *Repository) ListRecentChatContents(ctx context.Context, sessionID uuid.UUID, limit int) ([]string, error) {
	contents, err := r.db.ListRecentChatContentsBySessionID(ctx, database.ListRecentChatContentsBySessionIDParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: db ListRecentChatContentsBySessionID: %v", ErrInternal, err)
	}

	return contents, nil
}

func (r *Repository) GetSummary(ctx context.Context, sessionID uuid.UUID) (string, int, error) {
	row, err := r.db.GetSessionSummary(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrSummaryNotFound
		}
		return "", 0, fmt.Errorf("%w: db GetSessionSummary: %v", ErrInternal, err)
	}

	return row.Summary, int(row.Coveredchats), nil
}

func (r *Repository) UpsertSummary(ctx context.Context, sessionID uuid.UUID, summary string, covered int) error {
	if err := r.db.UpsertSessionSummary(ctx, database.UpsertSessionSummaryParams{
		Sessionid:    sessionID,
		Summary:      summary,
		Coveredchats: int32(covered),
		UpdateAt:     time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("%w: db UpsertSessionSummary: %v", ErrInternal, err)
	}

	return nil
}
