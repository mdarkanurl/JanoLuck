package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"
)

type Handler struct {
	service *Service
}

var validate = validator.New()

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

func init() {
	_ = validate.RegisterValidation("strong_password", strongPassword)
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

func validationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "email":
		return "must be a valid email address"
	case "min", "max":
		if _, ok := e.Value().(VerificationCode); ok {
			return "must be a 6-digit code"
		}
		switch e.Type().Kind() {
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
	case "strong_password":
		return "password must contain at least one letter and one number"
	default:
		return fmt.Sprintf("failed on '%s' validation", e.Tag())
	}
}

func validateRequest[T any](req T, w http.ResponseWriter) bool {
	if err := validate.Struct(req); err != nil {
		var fields []ValidationError

		for _, e := range err.(validator.ValidationErrors) {
			fields = append(fields, ValidationError{
				Field:   strings.ToLower(e.Field()),
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
	req, ok := decodeJSON[SignUpRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)

	if !validateRequest(req, w) {
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
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, AuthResponse{Message: "verify the mail", Data: nil})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[SignInRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)

	if !validateRequest(req, w) {
		return
	}

	tokens, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, VerifyEmailResponse{Message: "logged in", Data: *tokens})
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[VerifyEmailRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !validateRequest(req, w) {
		return
	}

	tokens, err := h.service.VerifyEmail(r.Context(), req.Email, int(req.Code))
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
	req, ok := decodeJSON[RefreshRequest](w, r)
	if !ok {
		return
	}

	if !validateRequest(req, w) {
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
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

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	if userID == "" {
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error())
		return
	}

	id, email, err := h.service.Me(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "ok",
		"data": map[string]string{
			"id":    id,
			"email": email,
		},
	})
}

func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[ForgotPasswordRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !validateRequest(req, w) {
		return
	}

	if err := h.service.ForgotPassword(r.Context(), req.Email); err != nil {
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{Message: "if the account exists, a reset code was sent", Data: nil})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[ResetPasswordRequest](w, r)
	if !ok {
		return
	}

	req.Email = normalizeEmail(req.Email)
	if !validateRequest(req, w) {
		return
	}

	err := h.service.ResetPassword(r.Context(), req.Email, int(req.Code), req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrTokenExpired):
			writeError(w, http.StatusGone, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{Message: "password reset successfully", Data: nil})
}

func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[ChangePasswordRequest](w, r)
	if !ok {
		return
	}

	if !validateRequest(req, w) {
		return
	}

	userID, userEmail, ok := UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	err := h.service.ChangePassword(r.Context(), userID, userEmail, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, AuthResponse{Message: "password changed successfully", Data: nil})
}
