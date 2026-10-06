// routes_auth.go registers authentication-related routes: login, registration, token exchange, password reset.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

// registerAuthRoutes registers authentication routes that require no auth
// (login, registration, token exchange, password reset, invitation accept).
// Login and password-reset requests are IP-rate-limited on top of that.
func (reg *routeRegistrar) registerAuthRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	authHandler := handler.NewAuthHandler(svc, s.Config.JWTSecret)
	loginRL := svcmw.RateLimitMiddleware(s.Redis, svcmw.LoginRateLimit, svcmw.IPKeyFunc)
	resetRL := svcmw.RateLimitMiddleware(s.Redis, svcmw.PasswordResetRateLimit, svcmw.IPKeyFunc)

	r.With(loginRL).Post("/auth/login", authHandler.Login)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/token-exchange", authHandler.TokenExchange)
	r.With(resetRL).Post("/auth/request-password-reset", authHandler.RequestPasswordReset)
	r.Post("/auth/reset-password", authHandler.ResetPassword)
	r.Post("/auth/accept-invitation", authHandler.AcceptInvitation)
}

// registerAuthProtectedRoutes registers authentication routes that require auth (identity query, workspace switch, password change).
func (reg *routeRegistrar) registerAuthProtectedRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	authHandler := handler.NewAuthHandler(svc, s.Config.JWTSecret)
	r.Get("/auth/whoami", authHandler.Whoami)
	r.Post("/auth/switch-workspace", authHandler.SwitchWorkspace)
	r.Post("/auth/change-password", authHandler.ChangePassword)
}
