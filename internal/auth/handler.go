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

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return v, false
	}
	return v, true
}

func (h *Handler) SignUp(w http.ResponseWriter, r *http.Request) {
	req, _ := decodeJSON[SignUpRequest](w, r)

	err := h.service.SignUp(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrUserExists):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrVerificationPending):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{Message: "verify the mail", Data: nil})
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	req, _ := decodeJSON[VerifyEmailRequest](w, r)

	tokens, err := h.service.VerifyEmail(r.Context(), req.Email, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			writeError(w, http.StatusGone, err.Error())
		case errors.Is(err, ErrUserExists):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, VerifyEmailResponse{Message: "email verified", Data: *tokens})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Accept the refresh JWT from either the JSON body or the HttpOnly cookie.
	var req RefreshRequest
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	refreshToken := req.RefreshToken
	if refreshToken == "" {
		if c, err := r.Cookie("refresh_token"); err == nil {
			refreshToken = c.Value
		}
	}
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh token required")
		return
	}

	tokens, err := h.service.Refresh(r.Context(), refreshToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			writeError(w, http.StatusUnauthorized, "refresh token expired, sign in again")
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, VerifyEmailResponse{Message: "tokens refreshed", Data: *tokens})
}
