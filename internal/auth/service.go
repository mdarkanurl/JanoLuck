package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mdarkanurl/JanoLuck/internal/database"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInternal           = errors.New("something went wrong")
)

type Service struct {
	repo *Repository
}

func AuthService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func validateCredentials(gmail, password string) error {
	if strings.TrimSpace(gmail) == "" || strings.TrimSpace(password) == "" {
		return ErrInvalidCredentials
	}
	if !strings.Contains(gmail, "@") {
		return ErrInvalidCredentials
	}
	if len(password) < 8 {
		return ErrInvalidCredentials
	}
	return nil
}

func (s *Service) SignUp(ctx context.Context, gmail, password string) (database.CreateUserRow, error) {
	if err := validateCredentials(gmail, password); err != nil {
		return database.CreateUserRow{}, err
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return database.CreateUserRow{}, ErrInternal
	}

	row, err := s.repo.CreateUser(ctx, gmail, string(hashed))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "23505") {
			return database.CreateUserRow{}, ErrUserExists
		}
		return database.CreateUserRow{}, ErrInternal
	}

	return row, nil
}

func (s *Service) SignIn(ctx context.Context, gmail, password string) (database.User, error) {
	if strings.TrimSpace(gmail) == "" || strings.TrimSpace(password) == "" {
		return database.User{}, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByGmail(ctx, gmail)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.User{}, ErrInvalidCredentials
		}
		return database.User{}, ErrInternal
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return database.User{}, ErrInvalidCredentials
	}

	return user, nil
}
