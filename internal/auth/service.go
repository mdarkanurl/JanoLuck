package auth

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrUserExists         = errors.New("user already exists")
	ErrInternal           = errors.New("something went wrong")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type Service struct {
	repo *Repository
}

func AuthService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func validateInput(email, password string) error {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
		return ErrInvalidInput
	}
	if !strings.Contains(email, "@") {
		return ErrInvalidInput
	}
	if len(password) < 8 {
		return ErrInvalidInput
	}
	return nil
}

func (s *Service) SignUp(ctx context.Context, email, password string) error {
	if err := validateInput(email, password); err != nil {
		return err
	}

	userInDB, err := s.repo.IsUserExistInDB(email, ctx)
	if err != nil {
		return err
	} else if userInDB == true {
		return nil
	}

	userInRedis, err := s.repo.IsUserExistInRedis(email, ctx)
	if err != nil {
		return err
	} else if userInRedis == true {
		return nil
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ErrInternal
	}

	if err := s.repo.CreateUserInRedis(ctx, email, string(hashed), rand.IntN(999999-100000+1)+100000); err != nil {
		return ErrInternal
	}

	return nil
}
