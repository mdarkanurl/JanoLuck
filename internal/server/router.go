package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
)

func NewRouter(
	authHandler *auth.Handler,
) chi.Router {
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK!"))
	})

	// auth route
	r.Route("/api/v1", func(r chi.Router) {

		// public routers
		r.Post("/auth/signup", authHandler.SignUp)
	})

	return r
}
