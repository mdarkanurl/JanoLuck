package embeddings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

var ErrNotFound = errors.New("chat not found")

type Repository struct {
	db *database.Queries
}

func NewRepository(dbQueries *database.Queries) *Repository {
	return &Repository{db: dbQueries}
}

func (r *Repository) UpdateEmbedding(ctx context.Context, chatID uuid.UUID, embedding string) error {
	updated, err := r.db.UpdateChatEmbedding(ctx, database.UpdateChatEmbeddingParams{
		ID:        chatID,
		Embedding: embedding,
		UpdateAt:  time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("embeddings: db UpdateChatEmbedding: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

type PendingChat struct {
	ID      uuid.UUID
	Content string
}

func (r *Repository) ListChats(ctx context.Context, includeAll bool) ([]PendingChat, error) {
	rows, err := r.db.ListChatsForEmbedding(ctx, includeAll)
	if err != nil {
		return nil, fmt.Errorf("embeddings: db ListChatsForEmbedding: %w", err)
	}

	chats := make([]PendingChat, 0, len(rows))
	for _, row := range rows {
		chats = append(chats, PendingChat{ID: row.ID, Content: row.Content})
	}
	return chats, nil
}
