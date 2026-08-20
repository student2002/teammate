// routes_auth.go 注册认证相关路由：登录、注册、Token 交换。
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
)

// registerAuthRoutes 注册无需认证的认证路由（登录、注册、Token 交换）。
func (reg *routeRegistrar) registerAuthRoutes(r chi.Router) {
	svc := reg.svc

	authHandler := handler.NewAuthHandler(svc, reg.server.Config.JWTSecret)
	r.Mount("/auth", authHandler.Routes())
}

// registerAuthProtectedRoutes 注册需要认证的认证路由（身份查询、工作区切换）。
func (reg *routeRegistrar) registerAuthProtectedRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	authHandler := handler.NewAuthHandler(svc, s.Config.JWTSecret)
	r.Get("/auth/whoami", authHandler.Whoami)
	r.Post("/auth/switch-workspace", authHandler.SwitchWorkspace)
}
