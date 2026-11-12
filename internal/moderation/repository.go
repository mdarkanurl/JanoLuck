package moderation

import (
	"context"
	"database/sql"
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

func (r *Repository) SetModeStatus(ctx context.Context, id uuid.UUID, status, reason string) error {
	reasonValue := sql.NullString{}
	if reason != "" {
		reasonValue = sql.NullString{String: reason, Valid: true}
	}

	updated, err := r.db.UpdateModeModeration(ctx, database.UpdateModeModerationParams{
		ID:               id,
		ModerationStatus: status,
		ModerationReason: reasonValue,
		UpdateAt:         time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("moderation: db UpdateModeModeration: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SetChatStatus(ctx context.Context, id uuid.UUID, status, reason string) error {
	reasonValue := sql.NullString{}
	if reason != "" {
		reasonValue = sql.NullString{String: reason, Valid: true}
	}

	updated, err := r.db.UpdateChatModeration(ctx, database.UpdateChatModerationParams{
		ID:               id,
		ModerationStatus: status,
		ModerationReason: reasonValue,
		UpdateAt:         time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("moderation: db UpdateChatModeration: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}
