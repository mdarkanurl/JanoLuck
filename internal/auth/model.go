package auth

type SignUpRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type VerifyEmailRequest struct {
	Email string `json:"email"`
	Code  int    `json:"code"`
}

type SignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
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
	RefreshToken string `json:"refreshToken"`
}

type VerifyEmailResponse struct {
	Message string     `json:"message"`
	Data    AuthTokens `json:"data"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
