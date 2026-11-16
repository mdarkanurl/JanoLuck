package modes

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

func pathParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	if value == "" {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return "", false
	}
	return value, true
}

func userIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return "", false
	}
	return userID, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	default:
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
	}
}

func toModeData(m database.Mode) ModeData {
	var userID string
	if m.Userid.Valid {
		userID = m.Userid.UUID.String()
	}

	return ModeData{
		ID:        m.ID.String(),
		UserID:    userID,
		Name:      m.Name,
		Prompt:    m.Prompt,
		IsBuiltIn: m.Isbuiltin,
		CreatedAt: m.CreateAt.UTC().Format(time.RFC3339),
		UpdatedAt: m.UpdateAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) CreateMode(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[CreateModeRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Prompt = strings.TrimSpace(req.Prompt)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	mode, err := h.service.CreateMode(r.Context(), userID, req.Name, req.Prompt)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, ModeResponse{
		Message: "mode created",
		Data:    toModeData(mode),
	})
}

func (h *Handler) ListModes(w http.ResponseWriter, r *http.Request) {
	page, limit, ok := httpx.ParsePagination(w, r)
	if !ok {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := h.service.ListModes(r.Context(), userID, page, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	items := make([]ModeData, 0, len(result.Modes))
	for _, mode := range result.Modes {
		items = append(items, toModeData(mode))
	}

	var activeModeID string
	if result.ActiveModeID != uuid.Nil {
		activeModeID = result.ActiveModeID.String()
	}

	httpx.WriteJSON(w, http.StatusOK, ModeListResponse{
		Message: "modes retrieved",
		Data: ModeListData{
			Modes:        items,
			Page:         page,
			Limit:        limit,
			Total:        result.Total,
			HasMore:      page*limit < result.Total,
			ActiveModeID: activeModeID,
		},
	})
}

func (h *Handler) GetMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	mode, err := h.service.GetMode(r.Context(), userID, modeID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeResponse{
		Message: "mode retrieved",
		Data:    toModeData(mode),
	})
}

func (h *Handler) UpdateMode(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[UpdateModeRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Prompt = strings.TrimSpace(req.Prompt)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	mode, err := h.service.UpdateMode(r.Context(), userID, modeID, req.Name, req.Prompt)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeResponse{
		Message: "mode updated",
		Data:    toModeData(mode),
	})
}

func (h *Handler) DeleteMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	if err := h.service.DeleteMode(r.Context(), userID, modeID); err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeActionResponse{
		Message: "mode deleted",
		Data: ModeActionData{
			ModeID: modeID,
			UserID: userID,
		},
	})
}

func (h *Handler) GetActiveMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	mode, err := h.service.GetActiveMode(r.Context(), userID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeResponse{
		Message: "active mode retrieved",
		Data:    toModeData(mode),
	})
}

func (h *Handler) SetActiveMode(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[SetActiveModeRequest](w, r)
	if !ok {
		return
	}

	req.ModeID = strings.TrimSpace(req.ModeID)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, err := h.service.SetActiveMode(r.Context(), userID, req.ModeID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeActionResponse{
		Message: "active mode updated",
		Data: ModeActionData{
			ModeID: modeID.String(),
			UserID: userID,
		},
	})
}

func (h *Handler) ClearActiveMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	if err := h.service.ClearActiveMode(r.Context(), userID); err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ModeActionResponse{
		Message: "active mode cleared",
		Data: ModeActionData{
			UserID: userID,
		},
	})
}
