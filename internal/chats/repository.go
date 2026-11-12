package chats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
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

func (r *Repository) CreateChat(ctx context.Context, sessionID, userID uuid.UUID, role, content string, embedding interface{}) (Chat, error) {
	row, err := r.db.CreateChat(ctx, database.CreateChatParams{
		Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
		Userid:    uuid.NullUUID{UUID: userID, Valid: true},
		Role:      sql.NullString{String: role, Valid: true},
		Content:   content,
		Embedding: embedding,
	})
	if err != nil {
		return Chat{}, fmt.Errorf("%w: db CreateChat: %v", ErrInternal, err)
	}

	return newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt), nil
}

func (r *Repository) CreateChatWithEvents(ctx context.Context, sessionID, userID uuid.UUID, role, content string, embedding interface{}, build func(Chat) []outbox.Event) (Chat, error) {
	var chat Chat

	err := r.db.InTx(ctx, func(q *database.Queries) error {
		row, err := q.CreateChat(ctx, database.CreateChatParams{
			Sessionid: uuid.NullUUID{UUID: sessionID, Valid: true},
			Userid:    uuid.NullUUID{UUID: userID, Valid: true},
			Role:      sql.NullString{String: role, Valid: true},
			Content:   content,
			Embedding: embedding,
		})
		if err != nil {
			return fmt.Errorf("%w: db CreateChat: %v", ErrInternal, err)
		}

		chat = newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt)

		store := outbox.NewRepository(q)
		for _, event := range build(chat) {
			if err := store.Enqueue(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
				return fmt.Errorf("%w: %v", ErrInternal, err)
			}
		}

		return nil
	})
	if err != nil {
		return Chat{}, err
	}

	return chat, nil
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

func (r *Repository) ListSimilarChatsBySessionID(ctx context.Context, sessionID, excludeChatID uuid.UUID, embedding string, limit int) ([]string, error) {
	chats, err := r.db.ListSimilarChatsBySessionID(ctx, database.ListSimilarChatsBySessionIDParams{
		Sessionid:     uuid.NullUUID{UUID: sessionID, Valid: true},
		Excludechatid: excludeChatID,
		Embedding:     embedding,
		Rowlimit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: db ListSimilarChatsBySessionID: %v", ErrInternal, err)
	}

	return chats, nil
}

func (r *Repository) GetActiveModeByUserID(ctx context.Context, userID uuid.UUID) (database.Mode, error) {
	mode, err := r.db.GetActiveModeByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Mode{}, ErrNoActiveMode
		}
		return database.Mode{}, fmt.Errorf("%w: db GetActiveModeByUserID: %v", ErrInternal, err)
	}

	return mode, nil
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

func (r *Repository) UpdateChatContentWithEvents(ctx context.Context, chatID, userID uuid.UUID, content string, build func(Chat) []outbox.Event) (Chat, error) {
	var chat Chat

	err := r.db.InTx(ctx, func(q *database.Queries) error {
		row, err := q.UpdateChatContent(ctx, database.UpdateChatContentParams{
			ID:       chatID,
			Userid:   uuid.NullUUID{UUID: userID, Valid: true},
			Content:  content,
			UpdateAt: time.Now().UTC(),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: db UpdateChatContent: %v", ErrInternal, err)
		}

		chat = newChat(row.ID, row.Sessionid, row.Userid, row.Role, row.Content, row.CreateAt, row.UpdateAt)

		store := outbox.NewRepository(q)
		for _, event := range build(chat) {
			if err := store.Enqueue(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
				return fmt.Errorf("%w: %v", ErrInternal, err)
			}
		}

		return nil
	})
	if err != nil {
		return Chat{}, err
	}

	return chat, nil
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

func (r *Repository) GetSessionSummary(ctx context.Context, sessionID uuid.UUID) (string, error) {
	row, err := r.db.GetSessionSummary(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("%w: db GetSessionSummary: %v", ErrInternal, err)
	}

	return row.Summary, nil
}

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
