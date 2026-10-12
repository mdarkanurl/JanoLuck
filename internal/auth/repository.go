package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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

func (r *Repository) CreateUserInDB(ctx context.Context, email, hashedPassword string) (database.User, error) {
	user, err := r.db.CreateUser(ctx, database.CreateUserParams{
		Email:    email,
		Password: hashedPassword,
	})
	if err != nil {
		return database.User{}, fmt.Errorf("%w: db CreateUser: %v", ErrInternal, err)
	}
	return database.User{
		ID:    user.ID,
		Email: user.Email,
	}, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (database.User, error) {
	user, err := r.db.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.User{}, ErrInvalidCredentials
		}
		return database.User{}, fmt.Errorf("%w: db GetUserByEmail: %v", ErrInternal, err)
	}
	return user, nil
}

func (r *Repository) GetUserByID(ctx context.Context, userID string) (database.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return database.User{}, fmt.Errorf("%w: invalid user id", ErrInvalidCredentials)
	}
	user, err := r.db.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.User{}, ErrInvalidCredentials
		}
		return database.User{}, fmt.Errorf("%w: db GetUserByID: %v", ErrInternal, err)
	}
	return database.User{
		ID:    user.ID,
		Email: user.Email,
	}, nil
}

type pendingUser struct {
	Email                string `json:"email"`
	Password             string `json:"password"`
	VerificationCodeHash string `json:"verificationCodeHash"`
}

func (r *Repository) GetPendingUser(ctx context.Context, email string) (pendingUser, error) {
	var pending pendingUser

	data, err := r.redis.Get(ctx, "verificationCode:"+email).Result()
	if err != nil {
		if err == redis.Nil {
			return pending, ErrTokenExpired
		}
		return pending, fmt.Errorf("%w: redis Get: %v", ErrInternal, err)
	}
	if data == "" {
		return pending, ErrTokenExpired
	}

	if err := json.Unmarshal([]byte(data), &pending); err != nil {
		return pending, fmt.Errorf("%w: unmarshal pending user: %v", ErrInternal, err)
	}

	return pending, nil
}

func (r *Repository) DeletePendingUser(ctx context.Context, email string) error {
	if err := r.redis.Del(ctx, "verificationCode:"+email).Err(); err != nil {
		return fmt.Errorf("%w: redis Del: %v", ErrInternal, err)
	}
	return nil
}

func (r *Repository) StoreRefreshToken(ctx context.Context, userID, refreshHash string, ttl time.Duration) error {
	if err := r.redis.Set(ctx, "refresh:"+refreshHash, userID, ttl).Err(); err != nil {
		return fmt.Errorf("%w: redis Set refresh token: %v", ErrInternal, err)
	}
	return nil
}

func (r *Repository) GetRefreshUserID(ctx context.Context, refreshHash string) (string, error) {
	userID, err := r.redis.Get(ctx, "refresh:"+refreshHash).Result()
	if err != nil {
		if err == redis.Nil {
			return "", ErrTokenExpired
		}
		return "", fmt.Errorf("%w: redis Get refresh token: %v", ErrInternal, err)
	}
	if userID == "" {
		return "", ErrTokenExpired
	}
	return userID, nil
}

func (r *Repository) DeleteRefreshToken(ctx context.Context, refreshHash string) error {
	if err := r.redis.Del(ctx, "refresh:"+refreshHash).Err(); err != nil {
		return fmt.Errorf("%w: redis Del refresh token: %v", ErrInternal, err)
	}
	return nil
}
