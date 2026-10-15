package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

var (
	ErrInvalidInput  = errors.New("invalid input")
	ErrInternal      = errors.New("something went wrong")
	ErrNotFound      = errors.New("profile not found")
	ErrAlreadyExists = errors.New("profile already exists")
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateProfile(ctx context.Context, userIDStr, name, qOne, qTwo string) (database.CreateProfileRow, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil || userID == uuid.Nil {
		return database.CreateProfileRow{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}

	if _, err := s.repo.GetProfileByUserID(ctx, userID); err == nil {
		return database.CreateProfileRow{}, ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return database.CreateProfileRow{}, err
	}

	questions := BuildFixedQuestions(qOne, qTwo)

	profile, err := s.repo.CreateProfile(ctx, userID, name, questions)
	if err != nil {
		return database.CreateProfileRow{}, err
	}

	return profile, nil
}
