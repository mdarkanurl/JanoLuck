package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[CreateProfileRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.QOne = strings.TrimSpace(req.QOne)
	req.QTwo = strings.TrimSpace(req.QTwo)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.CreateProfile(r.Context(), userID, req.Name, req.QOne, req.QTwo)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrAlreadyExists):
			httpx.WriteError(w, http.StatusConflict, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, ProfileResponse{
		Message: "profile created",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.GetProfile(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ProfileResponse{
		Message: "profile retrieved",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}

func (h *Handler) ProfileExists(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	exists, err := h.service.ProfileExists(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ProfileExistsResponse{
		Message: "profile status retrieved",
		Data: ProfileExistsData{
			Exists: exists,
			UserID: userID,
		},
	})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[UpdateProfileRequest](w, r)
	if !ok {
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
	}
	for i := range req.Questions {
		if req.Questions[i].Answer != nil {
			trimmed := strings.TrimSpace(*req.Questions[i].Answer)
			req.Questions[i].Answer = &trimmed
		}
	}

	if !httpx.ValidateRequest(req, w) {
		return
	}

	if req.Name == nil && len(req.Questions) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "provide name or questions to update")
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.UpdateProfile(r.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ProfileResponse{
		Message: "profile updated",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}
