package handler

import (
	"github.com/go-chi/chi/v5"
	"xray-panel/internal/app"
	"xray-panel/internal/middleware"
)

type Server struct {
	App *app.App
}

func NewServer(a *app.App) *Server {
	return &Server{App: a}
}

func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/api/auth/login", s.Login)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(s.App.Sessions()))

		r.Post("/api/auth/logout", s.Logout)
		r.Get("/api/auth/me", s.Me)
		r.Put("/api/auth/password", s.ChangePassword)

		r.Get("/api/inbounds", s.ListInbounds)
		r.Post("/api/inbounds", s.CreateInbound)
		r.Put("/api/inbounds/{tag}", s.UpdateInbound)
		r.Delete("/api/inbounds/{tag}", s.DeleteInbound)

		r.Get("/api/proxies", s.ListProxies)
		r.Post("/api/proxies", s.CreateProxy)
		r.Put("/api/proxies/{tag}", s.UpdateProxy)
		r.Delete("/api/proxies/{tag}", s.DeleteProxy)

		r.Get("/api/balancers", s.ListBalancers)
		r.Post("/api/balancers", s.CreateBalancer)
		r.Put("/api/balancers/{tag}", s.UpdateBalancer)
		r.Delete("/api/balancers/{tag}", s.DeleteBalancer)

		r.Get("/api/routing", s.GetRouting)
		r.Put("/api/routing", s.PutRouting)
		r.Get("/api/routing/templates", s.Templates)
		r.Get("/api/outbounds", s.Outbounds)

		r.Get("/api/subscription", s.GetSubscription)
		r.Put("/api/subscription", s.SetSubscription)
		r.Get("/api/nodes", s.ListNodes)
		r.Post("/api/nodes/test", s.TestNodes)

		r.Get("/api/xray/status", s.XrayStatus)
		r.Post("/api/apply", s.Apply)
		r.Post("/api/xray/restart", s.XrayRestart)
		r.Get("/api/config", s.RawConfig)
		r.Get("/api/topology", s.Topology)
	})

	return r
}
