package server

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/users"

	_ "github.com/lib/pq"
)

type Server struct {
	httpServer *http.Server
	router     chi.Router
	db         *sql.DB
}

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

	resendAPIKey := cfg.RESEND_API_KEY
	emailServer := email.NewResendService(resendAPIKey)

	dbQueries := database.New(db)
	redisQueries := database.NewRedisClient(cfg.REDIS_ADDR, cfg.REDIS_PASSWORD, redisDB)
	authRepo := auth.NewRepository(dbQueries, redisQueries)

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

	authService := auth.AuthService(authRepo, emailServer, tokenService)
	authHandler := auth.AuthHandler(authService)

	usersRepo := users.NewRepository(dbQueries)
	usersService := users.NewService(usersRepo)
	usersHandler := users.NewHandler(usersService)

	router := NewRouter(authHandler, tokenService, usersHandler)

	httpServer := &http.Server{
		Addr:    ":" + cfg.PORT,
		Handler: router,
	}

	return &Server{
		httpServer: httpServer,
		router:     router,
		db:         db,
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}
