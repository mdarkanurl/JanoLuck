package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/users"
)

func NewRouter(
	authHandler *auth.Handler,
	tokenService *auth.TokenService,
	usersHandler *users.Handler,
) chi.Router {
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK!"))
	})

	// auth route
	r.Route("/api/v1", func(r chi.Router) {

		// public routers
		r.Post("/auth/signup", authHandler.SignUp)
		r.Post("/auth/verify-email", authHandler.VerifyEmail)
		r.Post("/auth/login", authHandler.Login)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/auth/forgot-password", authHandler.ForgotPassword)
		r.Post("/auth/reset-password", authHandler.ResetPassword)

		// protected routes — require valid Bearer access token
		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware(tokenService))
			r.Get("/auth/me", authHandler.Me)
			r.Post("/auth/change-password", authHandler.ChangePassword)
			r.Post("/auth/logout", authHandler.Logout)

			r.Post("/profiles", usersHandler.CreateProfile)
			r.Get("/profiles", usersHandler.GetProfile)
			r.Put("/profiles", usersHandler.UpdateProfile)
		})
	})

	return r
}
