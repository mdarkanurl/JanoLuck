package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemoryLimiterAllowsUpToLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := NewMemoryLimiter()
	limiter.now = func() time.Time { return now }

	limit := Limit{Requests: 2, Window: time.Minute}
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		result, err := limiter.Allow(ctx, "ip:1", limit)
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i, err)
		}
		if !result.Allowed {
			t.Fatalf("attempt %d: expected request to be allowed", i)
		}
		if want := limit.Requests - i; result.Remaining != want {
			t.Fatalf("attempt %d: remaining = %d, want %d", i, result.Remaining, want)
		}
	}

	result, err := limiter.Allow(ctx, "ip:1", limit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected third request to be limited")
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("retry after = %v, want > 0", result.RetryAfter)
	}
}

func TestMemoryLimiterResetsAfterWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := NewMemoryLimiter()
	limiter.now = func() time.Time { return now }

	limit := Limit{Requests: 1, Window: time.Minute}
	ctx := context.Background()

	if result, _ := limiter.Allow(ctx, "ip:1", limit); !result.Allowed {
		t.Fatal("first request should be allowed")
	}
	if result, _ := limiter.Allow(ctx, "ip:1", limit); result.Allowed {
		t.Fatal("second request should be limited")
	}

	now = now.Add(time.Minute + time.Second)

	if result, _ := limiter.Allow(ctx, "ip:1", limit); !result.Allowed {
		t.Fatal("request after window should be allowed")
	}
}

func TestMemoryLimiterSeparatesKeys(t *testing.T) {
	limiter := NewMemoryLimiter()
	limit := Limit{Requests: 1, Window: time.Minute}
	ctx := context.Background()

	if result, _ := limiter.Allow(ctx, "ip:1", limit); !result.Allowed {
		t.Fatal("ip:1 first request should be allowed")
	}
	if result, _ := limiter.Allow(ctx, "ip:2", limit); !result.Allowed {
		t.Fatal("ip:2 should have its own bucket")
	}
}

func TestMemoryLimiterIgnoresInvalidLimit(t *testing.T) {
	limiter := NewMemoryLimiter()
	result, err := limiter.Allow(context.Background(), "ip:1", Limit{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("invalid limit should allow requests")
	}
}

func TestMiddlewareLimitsAndSetsHeaders(t *testing.T) {
	limiter := NewMemoryLimiter()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(MiddlewareConfig{
		Limiter: limiter,
		Limit:   Limit{Requests: 2, Window: time.Minute},
		Prefix:  "global",
	})(next)

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.10:4321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}
	if got := first.Header().Get("RateLimit-Limit"); got != "2" {
		t.Fatalf("RateLimit-Limit = %q, want 2", got)
	}
	if got := first.Header().Get("RateLimit-Remaining"); got != "1" {
		t.Fatalf("RateLimit-Remaining = %q, want 1", got)
	}

	if second := request(); second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200", second.Code)
	}

	third := request()
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("third status = %d, want 429", third.Code)
	}
	if third.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}

	var body map[string]string
	if err := json.NewDecoder(third.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "too many requests" {
		t.Fatalf("error = %q, want %q", body["error"], "too many requests")
	}
}

func TestMiddlewareSeparatesClients(t *testing.T) {
	limiter := NewMemoryLimiter()
	handler := Middleware(MiddlewareConfig{
		Limiter: limiter,
		Limit:   Limit{Requests: 1, Window: time.Minute},
		Prefix:  "auth",
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	send := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("203.0.113.1:1111"); code != http.StatusOK {
		t.Fatalf("first client status = %d, want 200", code)
	}
	if code := send("203.0.113.1:1111"); code != http.StatusTooManyRequests {
		t.Fatalf("first client second request status = %d, want 429", code)
	}
	if code := send("203.0.113.2:2222"); code != http.StatusOK {
		t.Fatalf("second client status = %d, want 200", code)
	}
}

func TestMiddlewareNilLimiterIsNoop(t *testing.T) {
	called := false
	handler := Middleware(MiddlewareConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusOK {
		t.Fatalf("nil limiter should pass through, called=%v status=%d", called, rec.Code)
	}
}

type failingLimiter struct{}

func (failingLimiter) Allow(context.Context, string, Limit) (Result, error) {
	return Result{}, errors.New("redis down")
}

func TestMiddlewareFailsOpenByDefault(t *testing.T) {
	handler := Middleware(MiddlewareConfig{
		Limiter: failingLimiter{},
		Limit:   Limit{Requests: 1, Window: time.Minute},
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMiddlewareFailsClosedWhenConfigured(t *testing.T) {
	handler := Middleware(MiddlewareConfig{
		Limiter:    failingLimiter{},
		Limit:      Limit{Requests: 1, Window: time.Minute},
		FailClosed: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestProxyClientIP(t *testing.T) {
	tests := []struct {
		name       string
		forwarded  string
		realIP     string
		remoteAddr string
		want       string
	}{
		{name: "forwarded", forwarded: "198.51.100.7, 10.0.0.1", remoteAddr: "10.0.0.2:1234", want: "198.51.100.7"},
		{name: "invalid forwarded ignored", forwarded: "not-an-ip, 198.51.100.8", remoteAddr: "10.0.0.2:1234", want: "198.51.100.8"},
		{name: "real ip", realIP: "198.51.100.9", remoteAddr: "10.0.0.2:1234", want: "198.51.100.9"},
		{name: "remote addr", remoteAddr: "10.0.0.2:1234", want: "10.0.0.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}
			if got := ProxyClientIP(req); got != tt.want {
				t.Fatalf("ProxyClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
