package auth

import (
	"context"
	"encoding/json"
	"strconv"
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

func (r *Repository) CreateUserInRedis(ctx context.Context, email, hashedPassword string, verificationCode int) error {
	data, err := json.Marshal(map[string]string{
		"email":            email,
		"password":         hashedPassword,
		"verificationCode": strconv.Itoa(verificationCode),
	})

	if err != nil {
		return ErrInternal
	}

	if _, err := r.redis.SetNX(ctx, "verificationCode:"+email, data, 5*time.Minute).Result(); err != nil {
		return err
	}

	return nil
}

func (r *Repository) IsUserExistInRedis(email string, ctx context.Context) (bool, error) {
	user, err := r.redis.Exists(ctx, "verificationCode:"+email).Result()

	if err != nil {
		return false, ErrInternal
	}

	if user > 0 {
		return true, nil
	} else {
		return false, nil
	}
}

func (r *Repository) IsUserExistInDB(email string, ctx context.Context) (bool, error) {
	user, err := r.db.UserExistsByEmail(ctx, email)
	if err != nil {
		return false, ErrInternal
	}

	return user, nil
}

func (r *Repository) CreateUserInDB(ctx context.Context, email, hashedPassword string) error {
	return r.db.CreateUser(ctx, database.CreateUserParams{
		Email:    email,
		Password: hashedPassword,
		UpdateAt: time.Now(),
	})
}
