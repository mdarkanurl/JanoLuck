package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const (
	TokenKey  string = "token"
	UserIDKey string = "userID"
	EmailKey  string = "email"
)

func AuthMiddleware(tokens *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeAuthError(w, "Authorization header is required", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAuthError(w, "Authorization header must be Bearer token", http.StatusUnauthorized)
				return
			}

			token := strings.TrimSpace(parts[1])
			if token == "" {
				writeAuthError(w, "Token is empty", http.StatusUnauthorized)
				return
			}

			userID, email, err := tokens.ValidateAccessToken(token)
			if err != nil {
				switch {
				case errors.Is(err, ErrTokenExpired):
					writeAuthError(w, "token expired", http.StatusUnauthorized)
				default:
					writeAuthError(w, "invalid or expired token", http.StatusUnauthorized)
				}
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, TokenKey, token)
			ctx = context.WithValue(ctx, UserIDKey, userID)
			ctx = context.WithValue(ctx, EmailKey, email)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok && userID != ""
}

func writeAuthError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}
