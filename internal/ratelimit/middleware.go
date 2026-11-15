package ratelimit

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type MiddlewareConfig struct {
	Limiter    Limiter
	Limit      Limit
	Prefix     string
	KeyFunc    func(*http.Request) string
	FailClosed bool
	OnError    func(*http.Request, error)
}

func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	if cfg.Limiter == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = ClientIP
	}

	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		prefix = "default"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := prefix + ":" + cfg.KeyFunc(r)

			result, err := cfg.Limiter.Allow(r.Context(), key, cfg.Limit)
			if err != nil {
				if cfg.OnError != nil {
					cfg.OnError(r, err)
				}
				if cfg.FailClosed {
					writeRateLimitError(w, http.StatusServiceUnavailable, "rate limiting unavailable")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("RateLimit-Limit", strconv.Itoa(result.Limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(result.Remaining))
			w.Header().Set("RateLimit-Reset", strconv.Itoa(int(math.Ceil(result.ResetAfter.Seconds()))))

			if !result.Allowed {
				retryAfter := int(math.Ceil(result.RetryAfter.Seconds()))
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeRateLimitError(w, http.StatusTooManyRequests, "too many requests")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	host = strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return "unknown"
	}
	return host
}

func ProxyClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		for _, part := range strings.Split(forwarded, ",") {
			if ip := strings.TrimSpace(part); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		if net.ParseIP(realIP) != nil {
			return realIP
		}
	}
	return ClientIP(r)
}
