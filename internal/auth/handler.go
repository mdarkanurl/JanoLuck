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
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.service.SignUp(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrUserExists):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrVerificationPending):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrInternal):
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{Message: "verify the mail", Data: nil})
}
