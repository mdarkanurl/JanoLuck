package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

const requestIDHeader = "X-Request-ID"

type requestIDContextKey struct{}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if !validRequestID(id) {
			id = newRequestID()
		}

		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf[:])
}

func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}

				logger.Error(
					"panic recovered",
					"panic", recovered,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", requestIDFromContext(r.Context()),
					"stack", string(debug.Stack()),
				)

				if written(w) {
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "something went wrong"})
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func AccessLog(logger *slog.Logger, clientIP func(*http.Request) string) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if clientIP == nil {
		clientIP = defaultClientIP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w}

			next.ServeHTTP(rw, r)

			status := rw.status
			if status == 0 {
				status = http.StatusOK
			}

			logger.Info(
				"http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", rw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", clientIP(r),
				"request_id", requestIDFromContext(r.Context()),
			)
		})
	}
}

type writtenReporter interface {
	Written() bool
}

func written(w http.ResponseWriter) bool {
	if reporter, ok := w.(writtenReporter); ok {
		return reporter.Written()
	}
	return false
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *responseWriter) Written() bool {
	return w.wroteHeader
}

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func defaultClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	host = strings.Trim(strings.TrimSpace(r.RemoteAddr), "[]")
	if host == "" {
		return "unknown"
	}
	return host
}

var _ http.Flusher = (*responseWriter)(nil)
