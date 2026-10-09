package auth

import (
	"context"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/database"
)

type Repository struct {
	queries *database.Queries
}

func NewRepository(queries *database.Queries) *Repository {
	return &Repository{queries: queries}
}

func (r *Repository) CreateUser(ctx context.Context, gmail, hashedPassword string) (database.CreateUserRow, error) {
	return r.queries.CreateUser(ctx, database.CreateUserParams{
		Gmail:    gmail,
		Password: hashedPassword,
		UpdateAt: time.Now(),
	})
}

func (r *Repository) GetUserByGmail(ctx context.Context, gmail string) (database.User, error) {
	return r.queries.GetUserByGmail(ctx, gmail)
}
