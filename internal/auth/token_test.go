package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestTokens(accessTTL, refreshTTL time.Duration) *TokenService {
	return NewTokenService("test-secret-key-for-unit-tests", accessTTL, refreshTTL)
}

func TestTokenService_AccessTokenRoundTrip(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	token, err := ts.GenerateAccessToken("user-123", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("GenerateAccessToken() returned empty token")
	}

	userID, email, err := ts.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if userID != "user-123" {
		t.Errorf("userID = %q, want %q", userID, "user-123")
	}
	if email != "user@example.com" {
		t.Errorf("email = %q, want %q", email, "user@example.com")
	}
}

func TestTokenService_RefreshTokenRoundTrip(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	token, err := ts.GenerateRefreshToken("user-123", "user@example.com")
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	userID, email, err := ts.ValidateRefreshToken(token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}
	if userID != "user-123" || email != "user@example.com" {
		t.Errorf("got (%q, %q), want (%q, %q)", userID, email, "user-123", "user@example.com")
	}
}

func TestTokenService_WrongTokenTypeRejected(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	access, err := ts.GenerateAccessToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := ts.GenerateRefreshToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := ts.ValidateRefreshToken(access); err == nil {
		t.Error("ValidateRefreshToken(accessToken) expected error, got nil")
	}
	if _, _, err := ts.ValidateAccessToken(refresh); err == nil {
		t.Error("ValidateAccessToken(refreshToken) expected error, got nil")
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	ts := newTestTokens(-time.Minute, -time.Hour)

	access, err := ts.GenerateAccessToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ts.ValidateAccessToken(access); err == nil {
		t.Error("expired access token should fail validation")
	} else if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}

	refresh, err := ts.GenerateRefreshToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ts.ValidateRefreshToken(refresh); err == nil {
		t.Error("expired refresh token should fail validation")
	} else if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestTokenService_TTLAccessors(t *testing.T) {
	ts := newTestTokens(15*time.Minute, 7*24*time.Hour)
	if ts.AccessTTL() != 15*time.Minute {
		t.Errorf("AccessTTL() = %v, want 15m", ts.AccessTTL())
	}
	if ts.RefreshTTL() != 7*24*time.Hour {
		t.Errorf("RefreshTTL() = %v, want 168h", ts.RefreshTTL())
	}
}

func TestTokenService_MalformedTokens(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	cases := map[string]string{
		"empty":         "",
		"single part":   "abc",
		"two parts":     "a.b",
		"four parts":    "a.b.c.d",
		"bad signature": "a.b.!!!not-base64!!!",
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ts.ValidateAccessToken(token); err == nil {
				t.Errorf("ValidateAccessToken(%q) expected error, got nil", token)
			}
		})
	}
}

func TestTokenService_TamperedTokenRejected(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	token, err := ts.GenerateAccessToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 token parts, got %d", len(parts))
	}

	tamperedPayload := parts[1]
	if tamperedPayload[0] == 'A' {
		tamperedPayload = "B" + tamperedPayload[1:]
	} else {
		tamperedPayload = "A" + tamperedPayload[1:]
	}
	tampered := parts[0] + "." + tamperedPayload + "." + parts[2]

	if _, _, err := ts.ValidateAccessToken(tampered); err == nil {
		t.Error("tampered token should fail validation")
	}
}

func TestTokenService_TokensAreUnique(t *testing.T) {
	ts := newTestTokens(time.Minute, time.Hour)

	a, err := ts.GenerateAccessToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ts.GenerateAccessToken("u1", "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two access tokens for the same user should differ (jti)")
	}
}

func TestHashRefreshToken(t *testing.T) {
	h1 := HashRefreshToken("token-a")
	h2 := HashRefreshToken("token-a")
	h3 := HashRefreshToken("token-b")

	if h1 != h2 {
		t.Error("HashRefreshToken must be deterministic")
	}
	if h1 == h3 {
		t.Error("different inputs must produce different hashes")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex sha256, got %d chars", len(h1))
	}
}
