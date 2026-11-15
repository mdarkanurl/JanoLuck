package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/ratelimit"
)

func TestRequestIDGeneratesAndStoresID(t *testing.T) {
	var gotFromContext string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	header := rec.Header().Get(requestIDHeader)
	if header == "" {
		t.Fatal("expected X-Request-ID response header")
	}
	if len(header) != 32 {
		t.Fatalf("request id length = %d, want 32 hex chars (%q)", len(header), header)
	}
	if gotFromContext != header {
		t.Fatalf("context id = %q, want header value %q", gotFromContext, header)
	}
}

func TestRequestIDReusesInboundID(t *testing.T) {
	const inbound = "req-abc-123"
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := requestIDFromContext(r.Context()); id != inbound {
			t.Fatalf("context id = %q, want %q", id, inbound)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(requestIDHeader, inbound)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got != inbound {
		t.Fatalf("response id = %q, want %q", got, inbound)
	}
}

func TestRequestIDReplacesUnsafeInboundID(t *testing.T) {
	tests := map[string]string{
		"control char": "abc\x01def",
		"too long":     strings.Repeat("a", 129),
	}

	for name, inbound := range tests {
		t.Run(name, func(t *testing.T) {
			handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			req.Header.Set(requestIDHeader, inbound)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Header().Get(requestIDHeader); got == inbound || got == "" {
				t.Fatalf("unsafe inbound id %q was not replaced (got %q)", inbound, got)
			}
		})
	}
}

func TestRecovererReturnsJSON500(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := RequestID(Recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected error message, got %v", body)
	}

	logged := logs.String()
	if !strings.Contains(logged, "panic recovered") {
		t.Fatalf("expected panic log, got %q", logged)
	}
	if !strings.Contains(logged, "request_id") {
		t.Fatalf("expected request_id in panic log, got %q", logged)
	}
}

func TestRecovererKeepsResponseWhenAlreadyWritten(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := AccessLog(logger, nil)(Recoverer(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("partial"))
		panic("boom after write")
	})))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 (already written)", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "partial")
	}
	if !strings.Contains(logs.String(), "panic recovered") {
		t.Fatalf("expected panic log, got %q", logs.String())
	}
}

func TestRecovererRepanicsAbortHandler(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Fatalf("recovered = %v, want http.ErrAbortHandler", recovered)
		}
	}()

	handler := Recoverer(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAccessLogRecordsRequestDetails(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := RequestID(AccessLog(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	})))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", nil)
	req.RemoteAddr = "203.0.113.7:5555"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v (raw %q)", err, logs.String())
	}

	if entry["msg"] != "http request" {
		t.Fatalf("msg = %v, want http request", entry["msg"])
	}
	if status, _ := entry["status"].(float64); int(status) != http.StatusCreated {
		t.Fatalf("status = %v, want 201", entry["status"])
	}
	if n, _ := entry["bytes"].(float64); int(n) != len("hello") {
		t.Fatalf("bytes = %v, want %d", entry["bytes"], len("hello"))
	}
	if entry["remote_ip"] != "203.0.113.7" {
		t.Fatalf("remote_ip = %v, want 203.0.113.7", entry["remote_ip"])
	}
	if entry["request_id"] != rec.Header().Get(requestIDHeader) {
		t.Fatalf("request_id = %v, want %q", entry["request_id"], rec.Header().Get(requestIDHeader))
	}
}

func TestAccessLogDefaultsStatusToOK(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := AccessLog(logger, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if status, _ := entry["status"].(float64); int(status) != http.StatusOK {
		t.Fatalf("status = %v, want 200", entry["status"])
	}
}

func TestResponseWriterPreservesFlusher(t *testing.T) {
	var flusherAvailable bool

	handler := AccessLog(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			flusher, ok := w.(http.Flusher)
			flusherAvailable = ok
			if !ok {
				return
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: hello\n\n"))
			flusher.Flush()
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sessions/1/chats/stream", nil))

	if !flusherAvailable {
		t.Fatal("access log wrapper removed http.Flusher support")
	}
	if !rec.Flushed {
		t.Fatal("expected the response to be flushed")
	}
	if rec.Body.String() != "data: hello\n\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestResponseWriterFlushRecordsStatus(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := AccessLog(logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Flushing before writing the header is what SSE handlers do.
		w.(http.Flusher).Flush()
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if status, _ := entry["status"].(float64); int(status) != http.StatusOK {
		t.Fatalf("status = %v, want 200", entry["status"])
	}
}

func TestRouterAddsRequestIDToRateLimitedResponse(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	router := NewRouter(
		auth.AuthHandler(nil),
		auth.NewTokenService("test-secret", time.Minute, time.Hour),
		nil, nil, nil, nil,
		WithRateLimiter(ratelimit.NewMemoryLimiter()),
		WithRateLimits(
			ratelimit.Limit{Requests: 1, Window: time.Minute},
			ratelimit.Limit{Requests: 1, Window: time.Minute},
		),
		WithRouterLogger(logger),
	)

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "203.0.113.11:4321"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(); rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", rec.Code)
	}
	rec := send()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", rec.Code)
	}
	if rec.Header().Get(requestIDHeader) == "" {
		t.Fatal("rate limited response is missing X-Request-ID")
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2 (%q)", len(lines), logs.String())
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if status, _ := entry["status"].(float64); int(status) != http.StatusTooManyRequests {
		t.Fatalf("logged status = %v, want 429", entry["status"])
	}
}
