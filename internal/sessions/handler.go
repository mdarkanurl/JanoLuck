package sessions

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func sessionIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sessionID == "" {
		httpx.WriteError(w, http.StatusNotFound, "session not found")
		return "", false
	}
	return sessionID, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	default:
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
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
	req, ok := httpx.DecodeJSON[CreateSessionRequest](w, r)
	if !ok {
		return
	}

	req.Content = strings.TrimSpace(req.Content)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.CreateSession(r.Context(), userID, req.Content)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, SessionResponse{
		Message: "session created",
		Data:    toSessionData(result),
	})
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	page, limit, ok := httpx.ParsePagination(w, r)
	if !ok {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.ListSessions(r.Context(), userID, page, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	sessions := make([]SessionData, 0, len(result.Sessions))
	for _, s := range result.Sessions {
		sessions = append(sessions, toSessionData(s))
	}

	httpx.WriteJSON(w, http.StatusOK, SessionListResponse{
		Message: "sessions retrieved",
		Data: SessionListData{
			UserID:   userID,
			Sessions: sessions,
			Page:     page,
			Limit:    limit,
			Total:    result.Total,
			HasMore:  page*limit < result.Total,
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
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, SessionResponse{
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
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if err := h.service.DeleteSession(r.Context(), userID, sessionID); err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, SessionActionResponse{
		Message: "session deleted",
		Data: SessionActionData{
			ID:     sessionID,
			UserID: userID,
		},
	})
}
