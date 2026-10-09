package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Handler struct {
	service *Service
}

func AuthHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func (h *Handler) SignUp(w http.ResponseWriter, r *http.Request) {
	var req SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	row, err := h.service.SignUp(r.Context(), req.Gmail, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrUserExists):
			writeError(w, http.StatusConflict, "user already exists")
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "invalid credentials")
		default:
			writeError(w, http.StatusInternalServerError, "something went wrong")
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{ID: row.ID, Gmail: row.Gmail})
}

func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	var req SignInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.service.SignIn(r.Context(), req.Gmail, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{ID: user.ID, Gmail: user.Gmail})
}
