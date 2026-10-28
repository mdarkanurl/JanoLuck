package chats

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

func (r *Repository) SessionExistsByUserID(ctx context.Context, sessionID, userID uuid.UUID) (bool, error) {
	exists, err := r.db.SessionExistsByUserID(ctx, database.SessionExistsByUserIDParams{
		ID:     sessionID,
		Userid: userID,
	})
	if err != nil {
		return false, fmt.Errorf("%w: db SessionExistsByUserID: %v", ErrInternal, err)
	}

	return exists, nil
}

func (r *Repository) CreateChat(ctx context.Context, sessionID, userID uuid.UUID, role, content string) (Chat, error) {
	row, err := r.db.CreateChat(ctx, database.CreateChatParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Userid:    uuid.NullUUID{UUID: userID, Valid: true},
		Role:      sql.NullString{String: role, Valid: true},
		Content:   content,
	})
	if err != nil {
		return Chat{}, fmt.Errorf("%w: db CreateChat: %v", ErrInternal, err)
	}

	return newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt), nil
}

func (r *Repository) ListChatsBySessionID(ctx context.Context, sessionID, userID uuid.UUID, limit, offset int) ([]Chat, error) {
	rows, err := r.db.ListChatsBySessionID(ctx, database.ListChatsBySessionIDParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Userid:    uuid.NullUUID{UUID: userID, Valid: true},
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: db ListChatsBySessionID: %v", ErrInternal, err)
	}

	chats := make([]Chat, 0, len(rows))
	for _, row := range rows {
		chats = append(chats, newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt))
	}

	return chats, nil
}

func (r *Repository) CountChatsBySessionID(ctx context.Context, sessionID, userID uuid.UUID) (int, error) {
	total, err := r.db.CountChatsBySessionID(ctx, database.CountChatsBySessionIDParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Userid:    uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("%w: db CountChatsBySessionID: %v", ErrInternal, err)
	}

	return int(total), nil
}

func (r *Repository) GetChatByID(ctx context.Context, chatID, userID uuid.UUID) (Chat, error) {
	row, err := r.db.GetChatByID(ctx, database.GetChatByIDParams{
		ID:     chatID,
		Userid: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Chat{}, ErrNotFound
		}
		return Chat{}, fmt.Errorf("%w: db GetChatByID: %v", ErrInternal, err)
	}

	return newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt), nil
}

func (r *Repository) UpdateChatContent(ctx context.Context, chatID, userID uuid.UUID, content string) (Chat, error) {
	row, err := r.db.UpdateChatContent(ctx, database.UpdateChatContentParams{
		ID:       chatID,
		Userid:   uuid.NullUUID{UUID: userID, Valid: true},
		Content:  content,
		UpdateAt: time.Now().UTC(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Chat{}, ErrNotFound
		}
		return Chat{}, fmt.Errorf("%w: db UpdateChatContent: %v", ErrInternal, err)
	}

	return newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt), nil
}

func (r *Repository) DeleteChat(ctx context.Context, chatID, userID uuid.UUID) error {
	deleted, err := r.db.DeleteChat(ctx, database.DeleteChatParams{
		ID:     chatID,
		Userid: uuid.NullUUID{UUID: userID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("%w: db DeleteChat: %v", ErrInternal, err)
	}

	if deleted == 0 {
		return ErrNotFound
	}

	return nil
}

// newChat unwraps the nullable columns produced by sqlc into the domain type.
func newChat(id uuid.UUID, sessionID, userID uuid.NullUUID, role sql.NullString, content string, createdAt, updatedAt time.Time) Chat {
	return Chat{
		ID:        id,
		SessionID: sessionID.UUID,
		UserID:    userID.UUID,
		Role:      role.String,
		Content:   content,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
