// routes_public.go 注册无需认证的公共路由。
package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

func (reg *routeRegistrar) registerPublicRoutes(r chi.Router) {
	s := reg.server
	svc := reg.svc

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Ping(); err != nil {
			slog.Warn("readiness check failed", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":"not_ready"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ready"}`)
	})

	r.Post("/api/webhooks/github", handler.NewWebhookHandler(svc).GitHub)

	r.With(svcmw.AuthMiddleware(s.Config.JWTSecret, reg.apiKeyAuth, s.Redis), svcmw.WorkspaceAuthMiddlewareWithChecker(reg.wsChk)).
		Get("/api/workspaces/{workspaceId}/runtimes/{runtimeId}/events", handler.NewSSEHandler(svc, s.Hub).Stream)

	r.Get("/api/tasks/{taskId}/logs/ws", handler.NewWSHandler(s.Gateway, s.Config.JWTSecret, svc, reg.wsChk).HandleWS)
}
