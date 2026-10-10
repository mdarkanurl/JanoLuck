package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/redis/go-redis/v9"
)

type Repository struct {
	db    *database.Queries
	redis *redis.Client
}

func NewRepository(dbQueries *database.Queries, redisQueries *redis.Client) *Repository {
	return &Repository{
		db:    dbQueries,
		redis: redisQueries,
	}
}

func (r *Repository) CreateUserInRedis(ctx context.Context, email, hashedPassword, verificationCodeHash string) error {
	data, err := json.Marshal(map[string]string{
		"email":                email,
		"password":             hashedPassword,
		"verificationCodeHash": verificationCodeHash,
	})

	if err != nil {
		return fmt.Errorf("%w: marshal pending user: %v", ErrInternal, err)
	}

	set, err := r.redis.SetNX(ctx, "verificationCode:"+email, data, 5*time.Minute).Result()
	if err != nil {
		return fmt.Errorf("%w: redis SetNX: %v", ErrInternal, err)
	}
	if !set {
		return ErrVerificationPending
	}

	return nil
}

func (r *Repository) IsUserExistInRedis(email string, ctx context.Context) (bool, error) {
	user, err := r.redis.Exists(ctx, "verificationCode:"+email).Result()

	if err != nil {
		return false, fmt.Errorf("%w: redis Exists: %v", ErrInternal, err)
	}

	return user > 0, nil
}

func (r *Repository) IsUserExistInDB(email string, ctx context.Context) (bool, error) {
	user, err := r.db.UserExistsByEmail(ctx, email)
	if err != nil {
		return false, fmt.Errorf("%w: db UserExistsByEmail: %v", ErrInternal, err)
	}

	return user, nil
}

func (r *Repository) CreateUserInDB(ctx context.Context, email, hashedPassword string) error {
	if err := r.db.CreateUser(ctx, database.CreateUserParams{
		Email:    email,
		Password: hashedPassword,
		UpdateAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("%w: db CreateUser: %v", ErrInternal, err)
	}
	return nil
}
