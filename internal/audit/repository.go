package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

type Repository struct {
	db *database.Queries
}

func NewRepository(dbQueries *database.Queries) *Repository {
	return &Repository{db: dbQueries}
}

func (r *Repository) Record(ctx context.Context, eventType string, event auth.UserEvent) error {
	metadata, err := json.Marshal(map[string]any{
		"occurred_at": event.OccurredAt,
	})
	if err != nil {
		return fmt.Errorf("audit: marshal metadata: %w", err)
	}

	userID := uuid.NullUUID{}
	if id, parseErr := uuid.Parse(event.UserID); parseErr == nil {
		userID = uuid.NullUUID{UUID: id, Valid: true}
	}

	email := sql.NullString{}
	if event.Email != "" {
		email = sql.NullString{String: event.Email, Valid: true}
	}

	if err := r.db.CreateAuditLog(ctx, database.CreateAuditLogParams{
		EventType: eventType,
		Userid:    userID,
		Email:     email,
		Metadata:  metadata,
	}); err != nil {
		return fmt.Errorf("audit: insert event %q: %w", eventType, err)
	}

	return nil
}
