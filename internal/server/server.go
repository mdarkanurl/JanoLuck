package server

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"

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

	dbQueries := database.New(db)
	redisQueries := database.NewRedisClient(cfg.REDIS_ADDR, cfg.REDIS_PASSWORD, redisDB)
	authRepo := auth.NewRepository(dbQueries, redisQueries)
	authService := auth.AuthService(authRepo)
	authHandler := auth.AuthHandler(authService)
	router := NewRouter(authHandler)

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
