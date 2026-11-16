package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/ratelimit"
)

type recordingLimiter struct {
	mu     sync.Mutex
	keys   []string
	limits []ratelimit.Limit
}

func (l *recordingLimiter) Allow(_ context.Context, key string, limit ratelimit.Limit) (ratelimit.Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.keys = append(l.keys, key)
	l.limits = append(l.limits, limit)

	return ratelimit.Result{
		Allowed:    true,
		Limit:      limit.Requests,
		Remaining:  limit.Requests,
		ResetAfter: limit.Window,
	}, nil
}

func (l *recordingLimiter) snapshot() ([]string, []ratelimit.Limit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.keys...), append([]ratelimit.Limit(nil), l.limits...)
}

func newTestRouter(limiter ratelimit.Limiter) http.Handler {
	return NewRouter(
		auth.AuthHandler(nil),
		auth.NewTokenService("test-secret", time.Minute, time.Hour),
		nil,
		nil,
		nil,
		nil,
		WithRateLimiter(limiter),
		WithRateLimits(
			ratelimit.Limit{Requests: 10, Window: time.Minute},
			ratelimit.Limit{Requests: 3, Window: time.Minute},
		),
	)
}

func TestRouterRateLimitsEveryRequest(t *testing.T) {
	limiter := &recordingLimiter{}
	router := newTestRouter(limiter)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}

	keys, limits := limiter.snapshot()
	if len(keys) != 1 {
		t.Fatalf("limiter calls = %d, want 1", len(keys))
	}
	if keys[0] != "global:203.0.113.5" {
		t.Fatalf("key = %q, want %q", keys[0], "global:203.0.113.5")
	}
	if limits[0].Requests != 10 {
		t.Fatalf("global limit = %d, want 10", limits[0].Requests)
	}
}

func TestRouterAuthResponsesAreNotCacheable(t *testing.T) {
	router := newTestRouter(&recordingLimiter{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{"))
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", got)
	}
}

func TestRouterDoesNotMarkOtherResponsesNoStore(t *testing.T) {
	router := newTestRouter(&recordingLimiter{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want empty", got)
	}
}

func TestRouterAppliesStricterLimitToAuthEndpoints(t *testing.T) {
	limiter := &recordingLimiter{}
	router := newTestRouter(limiter)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"nope"}`))
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("login status = %d, want 400", rec.Code)
	}

	keys, limits := limiter.snapshot()
	if len(keys) != 2 {
		t.Fatalf("limiter calls = %d, want 2 (global + auth)", len(keys))
	}
	if keys[0] != "global:203.0.113.5" || keys[1] != "auth:203.0.113.5" {
		t.Fatalf("keys = %v", keys)
	}
	if limits[1].Requests != 3 {
		t.Fatalf("auth limit = %d, want 3", limits[1].Requests)
	}
}

func TestRouterBlocksAfterLimitIsExhausted(t *testing.T) {
	router := newTestRouter(ratelimit.NewMemoryLimiter())

	send := func() int {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.9:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 10; i++ {
		if code := send(); code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i+1, code)
		}
	}

	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("request over limit status = %d, want 429", code)
	}
}
