package chats

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleUser   = "user"
	RoleSystem = "system"
)

type Chat struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	UserID    uuid.UUID
	Role      string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateChatRequest struct {
	Content string `json:"content" validate:"required,min=1,max=8000"`
}

type UpdateChatRequest struct {
	Content string `json:"content" validate:"required,min=1,max=8000"`
}

type ChatData struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	UserID    string `json:"userId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ChatResponse struct {
	Message string   `json:"message"`
	Data    ChatData `json:"data"`
}

type ChatListData struct {
	SessionID string     `json:"sessionId"`
	UserID    string     `json:"userId"`
	Chats     []ChatData `json:"chats"`
}

type ChatListResponse struct {
	Message string       `json:"message"`
	Data    ChatListData `json:"data"`
}

type ChatActionData struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
}

type ChatActionResponse struct {
	Message string         `json:"message"`
	Data    ChatActionData `json:"data"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
