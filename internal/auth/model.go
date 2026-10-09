package auth

import "github.com/google/uuid"

type SignUpRequest struct {
	Gmail    string `json:"gmail"`
	Password string `json:"password"`
}

type SignInRequest struct {
	Gmail    string `json:"gmail"`
	Password string `json:"password"`
}

type AuthResponse struct {
	ID    uuid.UUID `json:"id"`
	Gmail string    `json:"gmail"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
