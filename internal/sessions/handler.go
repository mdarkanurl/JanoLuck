package sessions

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
	"github.com/mdarkanurl/JanoLuck/internal/database"
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

func sessionIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sessionID == "" {
		writeError(w, http.StatusNotFound, "session not found")
		return "", false
	}
	return sessionID, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
	}
}

func toSessionData(s database.Session) SessionData {
	return SessionData{
		ID:        s.ID.String(),
		Name:      s.Name,
		UserID:    s.Userid.String(),
		CreatedAt: s.CreateAt.UTC().Format(time.RFC3339),
		UpdatedAt: s.UpdateAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateSessionRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)

	if !validateRequest(req, w) {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.CreateSession(r.Context(), userID, req.Name)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, SessionResponse{
		Message: "session created",
		Data:    toSessionData(result),
	})
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.ListSessions(r.Context(), userID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	sessions := make([]SessionData, 0, len(result))
	for _, s := range result {
		sessions = append(sessions, toSessionData(s))
	}

	writeJSON(w, http.StatusOK, SessionListResponse{
		Message: "sessions retrieved",
		Data: SessionListData{
			UserID:   userID,
			Sessions: sessions,
		},
	})
}

func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := sessionIDFromRequest(w, r)
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, SessionResponse{
		Message: "session retrieved",
		Data:    toSessionData(result),
	})
}

func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := sessionIDFromRequest(w, r)
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if err := h.service.DeleteSession(r.Context(), userID, sessionID); err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, SessionActionResponse{
		Message: "session deleted",
		Data: SessionActionData{
			ID:     sessionID,
			UserID: userID,
		},
	})
}
