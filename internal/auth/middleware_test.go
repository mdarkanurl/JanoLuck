package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testMiddlewareTokens() *TokenService {
	return NewTokenService("middleware-test-secret", time.Minute, time.Hour)
}

func runMiddleware(t *testing.T, ts *TokenService, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, email, ok := UserIDFromContext(r.Context())
		if !ok {
			http.Error(w, "missing context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(uid + ":" + email))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rr := httptest.NewRecorder()

	AuthMiddleware(ts)(next).ServeHTTP(rr, req)
	return rr
}

func decodeError(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	rr := runMiddleware(t, testMiddlewareTokens(), "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if got := decodeError(t, rr).Error; got == "" {
		t.Error("expected error message in body")
	}
}

func TestAuthMiddleware_Table(t *testing.T) {
	ts := testMiddlewareTokens()

	validAccess, err := ts.GenerateAccessToken("user-1", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	validRefresh, err := ts.GenerateRefreshToken("user-1", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	expiredTS := NewTokenService("middleware-test-secret", -time.Minute, -time.Hour)
	expiredAccess, err := expiredTS.GenerateAccessToken("user-1", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "valid bearer", header: "Bearer " + validAccess, wantStatus: http.StatusOK},
		{name: "lowercase bearer", header: "bearer " + validAccess, wantStatus: http.StatusOK},
		{name: "wrong scheme", header: "Basic " + validAccess, wantStatus: http.StatusUnauthorized},
		{name: "no scheme", header: validAccess, wantStatus: http.StatusUnauthorized},
		{name: "empty token", header: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "blank token", header: "Bearer    ", wantStatus: http.StatusUnauthorized},
		{name: "garbage token", header: "Bearer garbage.token.here", wantStatus: http.StatusUnauthorized},
		{name: "refresh token rejected", header: "Bearer " + validRefresh, wantStatus: http.StatusUnauthorized},
		{name: "expired token", header: "Bearer " + expiredAccess, wantStatus: http.StatusUnauthorized},
		{name: "tampered token", header: "Bearer " + validAccess + "x", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := runMiddleware(t, ts, tt.header)
			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
			}
		})
	}
}

func TestAuthMiddleware_PropagatesIdentity(t *testing.T) {
	ts := testMiddlewareTokens()
	token, err := ts.GenerateAccessToken("user-42", "forty@two.com")
	if err != nil {
		t.Fatal(err)
	}

	var ctxUser, ctxEmail string
	var ctxOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxUser, ctxEmail, ctxOK = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	AuthMiddleware(ts)(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !ctxOK {
		t.Fatal("UserIDFromContext() ok = false, want true")
	}
	if ctxUser != "user-42" || ctxEmail != "forty@two.com" {
		t.Errorf("context identity = (%q, %q), want (%q, %q)", ctxUser, ctxEmail, "user-42", "forty@two.com")
	}
}

func TestAuthMiddleware_ExpiredBody(t *testing.T) {
	ts := testMiddlewareTokens()
	expiredTS := NewTokenService("middleware-test-secret", -time.Minute, -time.Hour)
	token, _ := expiredTS.GenerateAccessToken("u", "e@x.com")

	rr := runMiddleware(t, ts, "Bearer "+token)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if got := decodeError(t, rr).Error; got != "token expired" {
		t.Errorf("error = %q, want %q", got, "token expired")
	}
}

func TestUserIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), UserIDKey, "uid-1")
	ctx = context.WithValue(ctx, EmailKey, "a@b.com")

	uid, email, ok := UserIDFromContext(ctx)
	if !ok || uid != "uid-1" || email != "a@b.com" {
		t.Errorf("got (%q, %q, %v), want (uid-1, a@b.com, true)", uid, email, ok)
	}

	if _, _, ok := UserIDFromContext(context.Background()); ok {
		t.Error("empty context should return ok=false")
	}

	emptyCtx := context.WithValue(context.Background(), UserIDKey, "")
	emptyCtx = context.WithValue(emptyCtx, EmailKey, "")
	if _, _, ok := UserIDFromContext(emptyCtx); ok {
		t.Error("empty userID/email should return ok=false")
	}
}
