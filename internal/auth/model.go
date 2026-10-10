package auth

type SignUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type SignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Message string `json:"message"`
	Data    []any  `json:"data"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
