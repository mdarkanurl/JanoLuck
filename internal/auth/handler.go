package auth

import (
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"
	"github.com/mdarkanurl/JanoLuck/internal/httpx"
)

type Handler struct {
	service *Service
}

func AuthHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func init() {
	_ = httpx.Validator.RegisterValidation("strong_password", strongPassword)
}

func strongPassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	var hasNumber, hasLetter bool
	for _, char := range password {
		switch {
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsLetter(char):
			hasLetter = true
		}
	}

	return hasNumber && hasLetter
}

func normalizeEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return strings.ToLower(email)
	}
	local := strings.ToLower(parts[0])
	domain := strings.ToLower(parts[1])
	return local + "@" + domain
}

func (h *Handler) SignUp(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[SignUpRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	err := h.service.SignUp(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrUserExists):
			httpx.WriteError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrVerificationPending):
			httpx.WriteError(w, http.StatusConflict, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, AuthResponse{Message: "verify the mail", Data: nil})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[SignInRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)

	if !httpx.ValidateRequest(req, w) {
		return
	}

	tokens, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, VerifyEmailResponse{Message: "logged in", Data: *tokens})
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[VerifyEmailRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !httpx.ValidateRequest(req, w) {
		return
	}

	tokens, err := h.service.VerifyEmail(r.Context(), req.Email, int(req.Code))
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			httpx.WriteError(w, http.StatusGone, err.Error())
		case errors.Is(err, ErrTooManyAttempts):
			w.Header().Set("Retry-After", "300")
			httpx.WriteError(w, http.StatusTooManyRequests, err.Error())
		case errors.Is(err, ErrUserExists):
			httpx.WriteError(w, http.StatusConflict, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, VerifyEmailResponse{Message: "email verified", Data: *tokens})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[RefreshRequest](w, r)
	if !ok {
		return
	}

	if !httpx.ValidateRequest(req, w) {
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			httpx.WriteError(w, http.StatusUnauthorized, "refresh token expired, sign in again")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, VerifyEmailResponse{Message: "tokens refreshed", Data: *tokens})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error())
		return
	}

	id, email, err := h.service.Me(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "ok",
		"data": map[string]string{
			"id":    id,
			"email": email,
		},
	})
}

func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[ForgotPasswordRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !httpx.ValidateRequest(req, w) {
		return
	}

	if err := h.service.ForgotPassword(r.Context(), req.Email); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Message: "if the account exists, a reset code was sent", Data: nil})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[ResetPasswordRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !httpx.ValidateRequest(req, w) {
		return
	}

	err := h.service.ResetPassword(r.Context(), req.Email, int(req.Code), req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			httpx.WriteError(w, http.StatusGone, err.Error())
		case errors.Is(err, ErrTooManyAttempts):
			w.Header().Set("Retry-After", "600")
			httpx.WriteError(w, http.StatusTooManyRequests, err.Error())
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Message: "password reset successfully", Data: nil})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	req, ok := httpx.DecodeJSON[ChangePasswordRequest](w, r)
	if !ok {
		return
	}

	if !httpx.ValidateRequest(req, w) {
		return
	}

	userID, userEmail, ok := UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	err := h.service.ChangePassword(r.Context(), userID, userEmail, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			httpx.WriteError(w, http.StatusUnauthorized, "current password is incorrect")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Message: "password changed successfully", Data: nil})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := UserIDFromContext(r.Context())
	if !ok || userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if err := h.service.Logout(r.Context(), userID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	httpx.WriteJSON(w, http.StatusOK, AuthResponse{Message: "logged out", Data: nil})
}
