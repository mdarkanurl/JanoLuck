package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/chats"
	"github.com/mdarkanurl/JanoLuck/internal/modes"
	"github.com/mdarkanurl/JanoLuck/internal/ratelimit"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"
	"github.com/mdarkanurl/JanoLuck/internal/users"
)

const (
	defaultRateLimitRequests     = 240
	defaultRateLimitWindow       = time.Minute
	defaultAuthRateLimitRequests = 10
	defaultAuthRateLimitWindow   = time.Minute
)

type routerConfig struct {
	limiter       ratelimit.Limiter
	trustProxy    bool
	rateLimit     ratelimit.Limit
	authRateLimit ratelimit.Limit
	logger        *slog.Logger
}

type RouterOption func(*routerConfig)

func WithRateLimiter(limiter ratelimit.Limiter) RouterOption {
	return func(c *routerConfig) {
		c.limiter = limiter
	}
}

func WithTrustProxy(trust bool) RouterOption {
	return func(c *routerConfig) {
		c.trustProxy = trust
	}
}

func WithRateLimits(global, authEndpoints ratelimit.Limit) RouterOption {
	return func(c *routerConfig) {
		c.rateLimit = global
		c.authRateLimit = authEndpoints
	}
}

func WithRouterLogger(logger *slog.Logger) RouterOption {
	return func(c *routerConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func NewRouter(
	authHandler *auth.Handler,
	tokenService *auth.TokenService,
	usersHandler *users.Handler,
	sessionsHandler *sessions.Handler,
	chatsHandler *chats.Handler,
	modesHandler *modes.Handler,
	opts ...RouterOption,
) chi.Router {
	cfg := routerConfig{
		limiter:       ratelimit.NewMemoryLimiter(),
		rateLimit:     ratelimit.Limit{Requests: defaultRateLimitRequests, Window: defaultRateLimitWindow},
		authRateLimit: ratelimit.Limit{Requests: defaultAuthRateLimitRequests, Window: defaultAuthRateLimitWindow},
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	useProxyHeaders := cfg.trustProxy
	clientKey := func(r *http.Request) string {
		if useProxyHeaders {
			return ratelimit.ProxyClientIP(r)
		}
		return ratelimit.ClientIP(r)
	}
	logLimiterError := func(r *http.Request, err error) {
		cfg.logger.Warn(
			"rate limiter unavailable; allowing request",
			"path", r.URL.Path,
			"error", err,
		)
	}

	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(AccessLog(cfg.logger, clientKey))
	r.Use(Recoverer(cfg.logger))

	r.Use(ratelimit.Middleware(ratelimit.MiddlewareConfig{
		Limiter: cfg.limiter,
		Limit:   cfg.rateLimit,
		Prefix:  "global",
		KeyFunc: clientKey,
		OnError: logLimiterError,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK!"))
	})

	// auth route
	r.Route("/api/v1", func(r chi.Router) {

		r.Group(func(r chi.Router) {
			r.Use(ratelimit.Middleware(ratelimit.MiddlewareConfig{
				Limiter: cfg.limiter,
				Limit:   cfg.authRateLimit,
				Prefix:  "auth",
				KeyFunc: clientKey,
				OnError: logLimiterError,
			}))

			r.Post("/auth/signup", authHandler.SignUp)
			r.Post("/auth/verify-email", authHandler.VerifyEmail)
			r.Post("/auth/login", authHandler.Login)
			r.Post("/auth/refresh", authHandler.Refresh)
			r.Post("/auth/forgot-password", authHandler.ForgotPassword)
			r.Post("/auth/reset-password", authHandler.ResetPassword)
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware(tokenService))
			r.Get("/auth/me", authHandler.Me)
			r.Post("/auth/change-password", authHandler.ChangePassword)
			r.Post("/auth/logout", authHandler.Logout)

			r.Post("/profiles", usersHandler.CreateProfile)
			r.Get("/profiles/exists", usersHandler.ProfileExists)
			r.Get("/profiles", usersHandler.GetProfile)
			r.Put("/profiles", usersHandler.UpdateProfile)

			r.Post("/sessions", sessionsHandler.CreateSession)
			r.Get("/sessions", sessionsHandler.ListSessions)
			r.Get("/sessions/{sessionID}", sessionsHandler.GetSession)
			r.Delete("/sessions/{sessionID}", sessionsHandler.DeleteSession)

			r.Post("/sessions/{sessionID}/chats", chatsHandler.CreateChat)
			r.Post("/sessions/{sessionID}/chats/stream", chatsHandler.CreateChatStream)
			r.Get("/sessions/{sessionID}/chats", chatsHandler.ListChats)

			r.Post("/modes", modesHandler.CreateMode)
			r.Get("/modes", modesHandler.ListModes)
			r.Get("/modes/active", modesHandler.GetActiveMode)
			r.Put("/modes/active", modesHandler.SetActiveMode)
			r.Delete("/modes/active", modesHandler.ClearActiveMode)
			r.Get("/modes/{modeID}", modesHandler.GetMode)
			r.Put("/modes/{modeID}", modesHandler.UpdateMode)
			r.Delete("/modes/{modeID}", modesHandler.DeleteMode)
		})
	})

	return r
}
