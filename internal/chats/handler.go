package chats

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
)

type Handler struct {
	service *Service
}

var validate = validator.New()

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg any) {
	switch v := msg.(type) {
	case ErrorResponse:
		writeJSON(w, status, v)
	case string:
		writeJSON(w, status, ErrorResponse{Error: v})
	case error:
		writeJSON(w, status, ErrorResponse{Error: v.Error()})
	default:
		writeJSON(w, status, ErrorResponse{Error: "internal server error"})
	}
}

func validationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", e.Field(), e.Param())
	case "min", "max":
		switch e.Type().Kind() {
		case reflect.Slice, reflect.Array:
			if e.Tag() == "min" {
				return fmt.Sprintf("must contain at least %s item(s)", e.Param())
			}
			return fmt.Sprintf("must contain at most %s item(s)", e.Param())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if e.Tag() == "min" {
				return fmt.Sprintf("must be at least %s", e.Param())
			}
			return fmt.Sprintf("must be at most %s", e.Param())
		}
		if e.Tag() == "min" {
			return fmt.Sprintf("must be at least %s characters", e.Param())
		}
		return fmt.Sprintf("must be at most %s characters", e.Param())
	case "dive":
		return "contains an invalid item"
	default:
		return fmt.Sprintf("failed on '%s' validation", e.Tag())
	}
}

func validateRequest[T any](req T, w http.ResponseWriter) bool {
	if err := validate.Struct(req); err != nil {
		var fields []ValidationError

		for _, e := range err.(validator.ValidationErrors) {
			field := strings.ToLower(e.StructField())
			if ns := e.Namespace(); strings.Contains(ns, "[") {
				field = strings.ToLower(ns[strings.Index(ns, ".")+1:])
			}
			fields = append(fields, ValidationError{
				Field:   field,
				Message: validationMessage(e),
			})
		}

		writeError(w, http.StatusBadRequest, ErrorResponse{
			Error:  "invalid_input",
			Fields: fields,
		})
		return false
	}
	return true
}

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return v, false
	}
	return v, true
}

func pathParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	if value == "" {
		writeError(w, http.StatusNotFound, "not found")
		return "", false
	}
	return value, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
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
	req, ok := decodeJSON[CreateChatRequest](w, r)
	if !ok {
		return
	}

	req.Role = strings.TrimSpace(req.Role)
	req.Content = strings.TrimSpace(req.Content)

	if !validateRequest(req, w) {
		return
	}

	sessionID, ok := pathParam(w, r, "sessionID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.CreateChat(r.Context(), userID, sessionID, req.Role, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, ChatResponse{
		Message: "message sent",
		Data:    toChatData(result),
	})
}

func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r, "sessionID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.ListChats(r.Context(), userID, sessionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	chatItems := make([]ChatData, 0, len(result))
	for _, c := range result {
		chatItems = append(chatItems, toChatData(c))
	}

	writeJSON(w, http.StatusOK, ChatListResponse{
		Message: "messages retrieved",
		Data: ChatListData{
			SessionID: sessionID,
			UserID:    userID,
			Chats:     chatItems,
		},
	})
}

func (h *Handler) GetChat(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathParam(w, r, "chatID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.GetChat(r.Context(), userID, chatID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Message: "message retrieved",
		Data:    toChatData(result),
	})
}

func (h *Handler) UpdateChat(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[UpdateChatRequest](w, r)
	if !ok {
		return
	}

	req.Content = strings.TrimSpace(req.Content)

	if !validateRequest(req, w) {
		return
	}

	chatID, ok := pathParam(w, r, "chatID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.UpdateChat(r.Context(), userID, chatID, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Message: "message updated",
		Data:    toChatData(result),
	})
}

func (h *Handler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	chatID, ok := pathParam(w, r, "chatID")
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if err := h.service.DeleteChat(r.Context(), userID, chatID); err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ChatActionResponse{
		Message: "message deleted",
		Data: ChatActionData{
			ID:     chatID,
			UserID: userID,
		},
	})
}
