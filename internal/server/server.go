package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	llm "github.com/mdarkanurl/JanoLuck/internal/LLM"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/chats"
	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"github.com/mdarkanurl/JanoLuck/internal/modes"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/ratelimit"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"
	"github.com/mdarkanurl/JanoLuck/internal/users"

	_ "github.com/lib/pq"
)

type Server struct {
	httpServer *http.Server
	router     chi.Router
	db         *sql.DB
	producer   *kafka.Producer
}

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverIdleTimeout       = 120 * time.Second
	serverMaxHeaderBytes    = 1 << 20 // 1 MiB
)

func New() *Server {
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DATABASE_URL)
	if err != nil {
		panic(err)
	}

	if err := db.Ping(); err != nil {
		panic(err)
	}

	redisDB, err := strconv.Atoi(cfg.REDIS_DB)
	if err != nil {
		panic(err)
	}

	dbQueries := database.New(db)
	redisQueries := database.NewRedisClient(cfg.REDIS_ADDR, cfg.REDIS_PASSWORD, redisDB)
	authRepo := auth.NewRepository(dbQueries, redisQueries)
	rateLimiter := ratelimit.NewRedisLimiter(redisQueries, "janoluck")

	accessMinutes, err := strconv.Atoi(cfg.ACCESS_TOKEN_TTL_MINUTES)
	if err != nil || accessMinutes <= 0 {
		accessMinutes = 15
	}
	refreshDays, err := strconv.Atoi(cfg.REFRESH_TOKEN_TTL_DAYS)
	if err != nil || refreshDays <= 0 {
		refreshDays = 30
	}
	tokenService := auth.NewTokenService(
		cfg.JWT_SECRET,
		time.Duration(accessMinutes)*time.Minute,
		time.Duration(refreshDays)*24*time.Hour,
	)

	producer, err := kafka.NewProducer(
		kafka.ParseBrokers(cfg.KAFKA_BROKERS),
		kafka.WithClientID(cfg.KAFKA_CLIENT_ID),
	)
	if err != nil {
		slog.Warn("kafka producer unavailable; signup verification emails cannot be queued", "error", err)
		producer = nil
	}

	authService := auth.AuthService(authRepo, tokenService, producer, auth.WithEventStore(outbox.NewRepository(dbQueries)))
	authHandler := auth.AuthHandler(authService)

	usersRepo := users.NewRepository(dbQueries)
	userOpts := []users.ServiceOption{
		users.WithEventStore(outbox.NewRepository(dbQueries)),
	}
	if producer != nil {
		userOpts = append(userOpts, users.WithProducer(producer))
	}
	usersService := users.NewService(usersRepo, userOpts...)
	usersHandler := users.NewHandler(usersService)

	sessionsRepo := sessions.NewRepository(dbQueries)
	sessionsLLM := llm.NewOpenRouterService(cfg.LLM_API_KEY, cfg.LLM_MODEL_NAME, cfg.LLM_EMBED_MODEL_NAME)
	sessionOpts := []sessions.ServiceOption{
		sessions.WithEventStore(outbox.NewRepository(dbQueries)),
	}
	if producer != nil {
		sessionOpts = append(sessionOpts, sessions.WithProducer(producer))
	}
	sessionsService := sessions.NewService(sessionsRepo, sessionsLLM, sessionOpts...)
	sessionsHandler := sessions.NewHandler(sessionsService)

	chatsRepo := chats.NewRepository(dbQueries)
	chatsLLM := llm.NewOpenRouterService(cfg.LLM_API_KEY, cfg.LLM_MODEL_NAME, cfg.LLM_EMBED_MODEL_NAME)
	chatsOpts := []chats.ServiceOption{
		chats.WithEventStore(outbox.NewRepository(dbQueries)),
	}
	if producer != nil {
		chatsOpts = append(chatsOpts, chats.WithProducer(producer))
	}
	chatsService := chats.NewService(chatsRepo, chatsLLM, chatsOpts...)
	chatsHandler := chats.NewHandler(chatsService)

	modesRepo := modes.NewRepository(dbQueries)
	modesOpts := []modes.ServiceOption{
		modes.WithEventStore(outbox.NewRepository(dbQueries)),
	}
	if producer != nil {
		modesOpts = append(modesOpts, modes.WithProducer(producer))
	}
	modesService := modes.NewService(modesRepo, modesOpts...)
	modesHandler := modes.NewHandler(modesService)

	router := NewRouter(
		authHandler,
		tokenService,
		usersHandler,
		sessionsHandler,
		chatsHandler,
		modesHandler,
		WithRateLimiter(rateLimiter),
		WithTrustProxy(strings.EqualFold(strings.TrimSpace(cfg.TRUST_PROXY), "true")),
		WithRateLimits(
			rateLimitFromConfig(cfg.RATE_LIMIT_REQUESTS, cfg.RATE_LIMIT_WINDOW_SECONDS, defaultRateLimitRequests, defaultRateLimitWindow),
			rateLimitFromConfig(cfg.AUTH_RATE_LIMIT_REQUESTS, cfg.AUTH_RATE_LIMIT_WINDOW_SECONDS, defaultAuthRateLimitRequests, defaultAuthRateLimitWindow),
		),
	)

	httpServer := &http.Server{
		Addr:              ":" + cfg.PORT,
		Handler:           router,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}

	return &Server{
		httpServer: httpServer,
		router:     router,
		db:         db,
		producer:   producer,
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		_ = s.httpServer.Close()
		return err
	}
	return nil
}

func (s *Server) Close() error {
	var err error
	if s.producer != nil {
		err = s.producer.Close()
	}
	if dbErr := s.db.Close(); dbErr != nil && err == nil {
		err = dbErr
	}
	return err
}

func rateLimitFromConfig(requestsRaw, windowRaw string, fallbackRequests int, fallbackWindow time.Duration) ratelimit.Limit {
	requests := fallbackRequests
	if v, err := strconv.Atoi(strings.TrimSpace(requestsRaw)); err == nil && v > 0 {
		requests = v
	}

	window := fallbackWindow
	if v, err := strconv.Atoi(strings.TrimSpace(windowRaw)); err == nil && v > 0 {
		window = time.Duration(v) * time.Second
	}

	return ratelimit.Limit{Requests: requests, Window: window}
}
