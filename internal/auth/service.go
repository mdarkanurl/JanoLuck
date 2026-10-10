package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrUserExists          = errors.New("user already exists")
	ErrVerificationPending = errors.New("verification already pending, check your mail")
	ErrInternal            = errors.New("something went wrong")
	ErrInvalidCredentials  = errors.New("invalid credentials")
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
	}
	if userInDB {
		return ErrUserExists
	}

	userInRedis, err := s.repo.IsUserExistInRedis(email, ctx)
	if err != nil {
		return err
	}
	if userInRedis {
		return ErrVerificationPending
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("%w: hash password: %v", ErrInternal, err)
	}

	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return fmt.Errorf("%w: generate verification code: %v", ErrInternal, err)
	}
	code := int(n.Int64()) + 100000
	sum := sha256.Sum256(fmt.Appendf(nil, "%06d", code))
	codeHash := hex.EncodeToString(sum[:])

	if err := s.repo.CreateUserInRedis(ctx, email, string(hashed), codeHash); err != nil {
		return err
	}

	return nil
}
