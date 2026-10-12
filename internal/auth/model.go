package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// VerificationCode is a 6-digit numeric code. It accepts both JSON numbers
// ({"code": 123456}) and JSON strings ({"code": "123456"}) because many
// clients serialize form input as strings.
type VerificationCode int

func (c *VerificationCode) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*c = VerificationCode(n)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("code must be a number or numeric string")
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("code must be a number or numeric string")
	}
	*c = VerificationCode(n)
	return nil
}

type SignUpRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254,lowercase"`
	Password string `json:"password" validate:"required,min=8,max=128,strong_password"`
}

type VerifyEmailRequest struct {
	Email string           `json:"email"    validate:"required,email,max=254,lowercase"`
	Code  VerificationCode `json:"code" validate:"required,min=100000,max=999999"`
}

type SignInRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254,lowercase"`
	Password string `json:"password" validate:"required,min=8,max=128,strong_password"`
}

type AuthResponse struct {
	Message string `json:"message"`
	Data    []any  `json:"data"`
}

type AuthTokens struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	TokenType        string `json:"tokenType"`
	ExpiresIn        int64  `json:"expiresIn"`
	RefreshExpiresIn int64  `json:"refreshExpiresIn"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=254,lowercase"`
}

type ResetPasswordRequest struct {
	Email       string           `json:"email"       validate:"required,email,max=254,lowercase"`
	Code        VerificationCode `json:"code"        validate:"required,min=100000,max=999999"`
	NewPassword string           `json:"newPassword" validate:"required,min=8,max=128,strong_password"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required,min=8,max=128"`
	NewPassword     string `json:"newPassword"     validate:"required,min=8,max=128,strong_password"`
}

type VerifyEmailResponse struct {
	Message string     `json:"message"`
	Data    AuthTokens `json:"data"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
