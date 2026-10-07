package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/config"
)

type Server struct {
	httpServer *http.Server
	router     chi.Router
}

func New() *Server {
	router := NewRouter()
	cfg := config.Load()

	httpServer := &http.Server{
		Addr:    ":" + cfg.PORT,
		Handler: router,
	}

	return &Server{
		httpServer: httpServer,
		router:     router,
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}
