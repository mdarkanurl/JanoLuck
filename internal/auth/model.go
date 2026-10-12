package auth

type SignUpRequest struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8,max=128,strong_password"`
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

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
