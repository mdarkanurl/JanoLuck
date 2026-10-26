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

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateChat(ctx context.Context, userIDStr, sessionIDStr, role, content string) (Chat, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return Chat{}, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return Chat{}, err
	}

	normalizedRole, normalizedContent, err := normalizeMessage(role, content)
	if err != nil {
		return Chat{}, err
	}
	role, content = normalizedRole, normalizedContent

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return Chat{}, err
	}

	isItFirstChat, err := s.isItFirstChat(ctx, sessionID)
	if !isItFirstChat {
		_, err := s.llm.GenerateSessionName(content)
		if err != nil {
			// handle the error
		}

	}

	return s.repo.CreateChat(ctx, sessionID, userID, role, content)
}

func (s *Service) ListChats(ctx context.Context, userIDStr, sessionIDStr string) ([]Chat, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return nil, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return nil, err
	}

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return nil, err
	}

	return s.repo.ListChatsBySessionID(ctx, sessionID, userID)
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

func (s *Service) isItFirstChat(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	exists, err := s.repo.IsItFirstChat(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, ErrNotFirstChat
	}

	return exists, nil
}

func normalizeMessage(role, content string) (string, string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	content = strings.TrimSpace(content)

	if role != RoleUser && role != RoleSystem {
		return "", "", fmt.Errorf("%w: role must be 'user' or 'system'", ErrInvalidInput)
	}
	if content == "" {
		return "", "", fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if len(content) > 8000 {
		return "", "", fmt.Errorf("%w: content must be at most 8000 characters", ErrInvalidInput)
	}

	return role, content, nil
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
