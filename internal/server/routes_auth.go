// routes_auth.go registers authentication-related routes: login, registration, token exchange.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
)

// registerAuthRoutes registers authentication routes that require no auth (login, registration, token exchange).
func (reg *routeRegistrar) registerAuthRoutes(r chi.Router) {
	svc := reg.svc

	authHandler := handler.NewAuthHandler(svc, reg.server.Config.JWTSecret)
	r.Mount("/auth", authHandler.Routes())
}

// registerAuthProtectedRoutes registers authentication routes that require auth (identity query, workspace switch).
func (reg *routeRegistrar) registerAuthProtectedRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	authHandler := handler.NewAuthHandler(svc, s.Config.JWTSecret)
	r.Get("/auth/whoami", authHandler.Whoami)
	r.Post("/auth/switch-workspace", authHandler.SwitchWorkspace)
}
