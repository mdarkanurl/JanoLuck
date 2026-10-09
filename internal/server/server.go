package server

import (
	"database/sql"
	"net/http"

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

	queries := database.New(db)
	authRepo := auth.NewRepository(queries)
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
