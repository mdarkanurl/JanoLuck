package chats

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func pathParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	if value == "" {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return "", false
	}
	return value, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrSessionNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	default:
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
	}
}

func toChatData(c Chat) ChatData {
	return ChatData{
		ID:        c.ID.String(),
		SessionID: c.SessionID.String(),
		UserID:    c.UserID.String(),
		Role:      c.Role,
		Content:   c.Content,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[CreateChatRequest](w, r)
	if !ok {
		return
	}

	req.Content = strings.TrimSpace(req.Content)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	sessionIDStr, ok := pathParam(w, r, "sessionID")
	if !ok {
		return
	}

	userIDStr, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userIDStr == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, ErrInvalidInput.Error())
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, ErrInvalidInput.Error())
		return
	}

	result, err := h.service.CreateChat(r.Context(), userID, sessionID, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, ChatResponse{
		Message: "message sent",
		Data:    toChatData(result),
	})
}

type chatStreamEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	started bool
}

func (e *chatStreamEmitter) Start() error {
	flusher, ok := e.w.(http.Flusher)
	if !ok {
		return errors.New("streaming is not supported by the response writer")
	}

	e.flusher = flusher
	e.w.Header().Set("Content-Type", "text/event-stream")
	e.w.Header().Set("Cache-Control", "no-cache")
	e.w.Header().Set("X-Accel-Buffering", "no")
	e.w.WriteHeader(http.StatusOK)
	e.flusher.Flush()
	e.started = true

	return nil
}

func (e *chatStreamEmitter) Delta(text string) error {
	return e.write(ChatStreamPayload{Type: StreamEventDelta, Content: text})
}

func (e *chatStreamEmitter) Done(chat Chat) error {
	data := toChatData(chat)

	return e.write(ChatStreamPayload{Type: StreamEventDone, Chat: &data})
}

func (e *chatStreamEmitter) Error(message string) error {
	return e.write(ChatStreamPayload{Type: StreamEventError, Error: message})
}

func (e *chatStreamEmitter) write(payload ChatStreamPayload) error {
	if !e.started {
		return errors.New("the stream has not started")
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", raw); err != nil {
		return err
	}

	e.flusher.Flush()

	return nil
}

// streamErrorMessage keeps the internal details of a failure private while
// still telling the client what went wrong.
func streamErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrInvalidInput),
		errors.Is(err, ErrNotFound),
		errors.Is(err, ErrSessionNotFound):
		return err.Error()
	default:
		return ErrInternal.Error()
	}
}

// CreateChatStream stores the message and streams the prediction back while it
// is generated.
func (h *Handler) CreateChatStream(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[CreateChatRequest](w, r)
	if !ok {
		return
	}

	req.Content = strings.TrimSpace(req.Content)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	sessionIDStr, ok := pathParam(w, r, "sessionID")
	if !ok {
		return
	}

	userIDStr, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userIDStr == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, ErrInvalidInput.Error())
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, ErrInvalidInput.Error())
		return
	}

	emitter := &chatStreamEmitter{w: w}

	if _, err := h.service.CreateChatStream(r.Context(), userID, sessionID, req.Content, emitter); err != nil {
		if !emitter.started {
			writeServiceError(w, err)
			return
		}

		_ = emitter.Error(streamErrorMessage(err))
	}
}

func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	page, limit, ok := httpx.ParsePagination(w, r)
	if !ok {
		return
	}

	sessionID, ok := pathParam(w, r, "sessionID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.ListChats(r.Context(), userID, sessionID, page, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	chatItems := make([]ChatData, 0, len(result.Chats))
	for _, c := range result.Chats {
		chatItems = append(chatItems, toChatData(c))
	}

	httpx.WriteJSON(w, http.StatusOK, ChatListResponse{
		Message: "messages retrieved",
		Data: ChatListData{
			SessionID: sessionID,
			UserID:    userID,
			Chats:     chatItems,
			Page:      page,
			Limit:     limit,
			Total:     result.Total,
			HasMore:   page*limit < result.Total,
		},
	})
}
