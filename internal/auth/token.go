package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenService(secret string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (t *TokenService) RefreshTTL() time.Duration {
	return t.refreshTTL
}

func (t *TokenService) AccessTTL() time.Duration {
	return t.accessTTL
}

type tokenClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Type  string `json:"typ"`
	Exp   int64  `json:"exp"`
	Iat   int64  `json:"iat"`
	Jti   string `json:"jti"`
}

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

func (t *TokenService) sign(userID, email, typ string, ttl time.Duration) (string, error) {
	now := time.Now()
	var jtiBytes [16]byte
	if _, err := rand.Read(jtiBytes[:]); err != nil {
		return "", fmt.Errorf("%w: generate token id: %v", ErrInternal, err)
	}
	payload, err := json.Marshal(tokenClaims{
		Sub:   userID,
		Email: email,
		Type:  typ,
		Exp:   now.Add(ttl).Unix(),
		Iat:   now.Unix(),
		Jti:   hex.EncodeToString(jtiBytes[:]),
	})
	if err != nil {
		return "", fmt.Errorf("%w: marshal %s claims: %v", ErrInternal, typ, err)
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := header + "." + body

	mac := hmac.New(sha256.New, t.secret)
	_, _ = mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

func (t *TokenService) GenerateAccessToken(userID, email string) (string, error) {
	return t.sign(userID, email, tokenTypeAccess, t.accessTTL)
}

func (t *TokenService) GenerateRefreshToken(userID, email string) (string, error) {
	return t.sign(userID, email, tokenTypeRefresh, t.refreshTTL)
}

func (t *TokenService) parse(token, wantType string) (userID, email string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("%w: malformed token", ErrInvalidCredentials)
	}

	mac := hmac.New(sha256.New, t.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := mac.Sum(nil)

	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed token signature", ErrInvalidCredentials)
	}
	if !hmac.Equal(got, expected) {
		return "", "", fmt.Errorf("%w: invalid token signature", ErrInvalidCredentials)
	}

	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed token payload", ErrInvalidCredentials)
	}

	var claims tokenClaims
	if err := json.Unmarshal(rawPayload, &claims); err != nil {
		return "", "", fmt.Errorf("%w: malformed token claims", ErrInvalidCredentials)
	}
	if claims.Type != wantType {
		return "", "", fmt.Errorf("%w: wrong token type", ErrInvalidCredentials)
	}
	if time.Now().Unix() > claims.Exp {
		return "", "", ErrTokenExpired
	}

	return claims.Sub, claims.Email, nil
}

func (t *TokenService) ValidateAccessToken(token string) (userID, email string, err error) {
	return t.parse(token, tokenTypeAccess)
}

func (t *TokenService) ValidateRefreshToken(token string) (userID, email string, err error) {
	return t.parse(token, tokenTypeRefresh)
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
