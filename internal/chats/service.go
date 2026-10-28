package chats

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	llm "github.com/mdarkanurl/JanoLuck/internal/LLM"
)

var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrInternal        = errors.New("something went wrong")
	ErrNotFound        = errors.New("chat not found")
	ErrSessionNotFound = errors.New("session not found")
	ErrNotFirstChat    = errors.New("It is not first chat")
)

type Service struct {
	repo *Repository
	llm  *llm.LLM
}

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxPage      = 1_000_000
	MaxLimit     = 100
)

func NewService(repo *Repository, llm *llm.LLM) *Service {
	return &Service{repo: repo, llm: llm}
}

func (s *Service) CreateChat(ctx context.Context, userIDStr, sessionIDStr, content string) (Chat, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return Chat{}, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return Chat{}, err
	}

	normalizedContent, err := normalizeMessage(content)
	if err != nil {
		return Chat{}, err
	}
	content = normalizedContent

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return Chat{}, err
	}

	// Save the incoming message
	if _, err := s.repo.CreateChat(ctx, sessionID, userID, RoleUser, content); err != nil {
		return Chat{}, err
	}

	// Generate prediction by LLM
	prediction, err := s.llm.GeneratePrediction(content)
	if err != nil {
		return Chat{}, fmt.Errorf("%w: llm GeneratePrediction: %v", ErrInternal, err)
	}

	return s.repo.CreateChat(ctx, sessionID, userID, RoleSystem, prediction)
}

func (s *Service) ListChats(ctx context.Context, userIDStr, sessionIDStr string, page, limit int) (ListResult, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return ListResult{}, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return ListResult{}, err
	}

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return ListResult{}, err
	}

	if page < 1 || page > MaxPage {
		return ListResult{}, fmt.Errorf("%w: page must be between 1 and %d", ErrInvalidInput, MaxPage)
	}
	if limit < 1 || limit > MaxLimit {
		return ListResult{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, MaxLimit)
	}

	total, err := s.repo.CountChatsBySessionID(ctx, sessionID, userID)
	if err != nil {
		return ListResult{}, err
	}

	chats, err := s.repo.ListChatsBySessionID(ctx, sessionID, userID, limit, (page-1)*limit)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{Chats: chats, Total: total}, nil
}

func (s *Service) GetChat(ctx context.Context, userIDStr, chatIDStr string) (Chat, error) {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return Chat{}, err
	}

	return s.repo.GetChatByID(ctx, chatID, userID)
}

func (s *Service) UpdateChat(ctx context.Context, userIDStr, chatIDStr, content string) (Chat, error) {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return Chat{}, err
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return Chat{}, fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if len(content) > 8000 {
		return Chat{}, fmt.Errorf("%w: content must be at most 8000 characters", ErrInvalidInput)
	}

	return s.repo.UpdateChatContent(ctx, chatID, userID, content)
}

func (s *Service) DeleteChat(ctx context.Context, userIDStr, chatIDStr string) error {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return err
	}

	return s.repo.DeleteChat(ctx, chatID, userID)
}

func (s *Service) ensureSessionExists(ctx context.Context, sessionID, userID uuid.UUID) error {
	exists, err := s.repo.SessionExistsByUserID(ctx, sessionID, userID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSessionNotFound
	}

	return nil
}

func normalizeMessage(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if len(content) > 8000 {
		return "", fmt.Errorf("%w: content must be at most 8000 characters", ErrInvalidInput)
	}

	return content, nil
}

func parseIDs(userIDStr, chatIDStr, field string) (uuid.UUID, uuid.UUID, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	chatID, err := parseUUID(chatIDStr, field)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return userID, chatID, nil
}

func parseUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s", ErrInvalidInput, field)
	}

	return id, nil
}
