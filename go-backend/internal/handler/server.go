package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		r.Get("/api/inbounds/{tag}/share", s.ShareInbound)
		r.Get("/api/tools/gen", s.GenSecret)

		r.Get("/api/proxies", s.ListProxies)
		r.Post("/api/proxies", s.CreateProxy)
		r.Put("/api/proxies/{tag}", s.UpdateProxy)
		r.Delete("/api/proxies/{tag}", s.DeleteProxy)

		r.Get("/api/balancers", s.ListBalancers)
		r.Get("/api/balancers/status", s.GroupStatus)
		r.Post("/api/balancers/preview", s.PreviewGroup)
		r.Put("/api/balancers/{tag}/select", s.SelectGroup)
		r.Post("/api/balancers", s.CreateBalancer)
		r.Put("/api/balancers/{tag}", s.UpdateBalancer)
		r.Delete("/api/balancers/{tag}", s.DeleteBalancer)

		r.Get("/api/routing", s.GetRouting)
		r.Put("/api/routing", s.PutRouting)
		r.Get("/api/routing/templates", s.Templates)
		r.Post("/api/routing/test", s.TestRoute)
		r.Get("/api/outbounds", s.Outbounds)

		r.Get("/api/subscriptions", s.ListSubscriptions)
		r.Post("/api/subscriptions", s.CreateSubscription)
		r.Put("/api/subscriptions/{id}", s.UpdateSubscription)
		r.Delete("/api/subscriptions/{id}", s.DeleteSubscription)
		r.Post("/api/subscriptions/{id}/fetch", s.FetchSubscription)
		r.Post("/api/subscriptions/fetch-all", s.FetchAllSubscriptions)
		r.Get("/api/nodes", s.ListNodes)
		r.Post("/api/nodes/test", s.TestNodes)
		r.Post("/api/nodes/batch", s.BatchNodes)
		r.Put("/api/nodes/{tag}", s.UpdateNode)

		r.Get("/api/xray/status", s.XrayStatus)
		r.Post("/api/apply", s.Apply)
		r.Post("/api/xray/restart", s.XrayRestart)
		r.Get("/api/xray/logs", s.XrayLogs)
		r.Get("/api/config", s.RawConfig)
		r.Get("/api/config/draft", s.DraftConfig)
		r.Get("/api/config/diff", s.DraftDiff)
		r.Get("/api/traffic", s.Traffic)
		r.Get("/api/online", s.Online)
		r.Get("/api/rulesets", s.ListRuleSets)
		r.Post("/api/rulesets", s.CreateRuleSet)
		r.Put("/api/rulesets/{tag}", s.UpdateRuleSet)
		r.Delete("/api/rulesets/{tag}", s.DeleteRuleSet)
		r.Post("/api/rulesets/{tag}/refresh", s.RefreshRuleSet)
		r.Get("/api/rulesets/{tag}/entries", s.RuleSetEntries)
		r.Get("/api/geo/catalog", s.GeoCatalog)
		r.Get("/api/geo/entries", s.GeoEntries)
		r.Get("/api/users", s.ListUsers)
		r.Post("/api/users", s.CreateUser)
		r.Put("/api/users/{name}", s.UpdateUser)
		r.Delete("/api/users/{name}", s.DeleteUser)
		r.Get("/api/users/{name}/share", s.ShareUser)
		r.Get("/api/history", s.ListHistory)
		r.Get("/api/history/{id}/diff", s.HistoryDiff)
		r.Post("/api/history/{id}/restore", s.RestoreHistory)
		r.Get("/api/backup", s.ExportBackup)
		r.Post("/api/backup/restore", s.ImportBackup)
		r.Get("/api/settings", s.GetSettings)
		r.Put("/api/settings", s.PutSettings)
		r.Post("/api/settings/notify/test", s.TestNotify)
		r.Post("/api/geo/update", s.UpdateGeo)
		r.Get("/api/topology", s.Topology)
	})

	// SPA static file serving (mirrors Python _mount_static)
	frontendDir := "frontend_dist"
	if info, err := os.Stat(frontendDir); err == nil && info.IsDir() {
		assetsDir := filepath.Join(frontendDir, "assets")
		if info, err := os.Stat(assetsDir); err == nil && info.IsDir() {
			fs := http.FileServer(http.Dir(assetsDir))
			r.Handle("/assets/*", http.StripPrefix("/assets/", fs))
		}
		// SPA fallback: non-/api/ paths → index.html
		indexPath := filepath.Join(frontendDir, "index.html")
		r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, 404, "not found")
				return
			}
			http.ServeFile(w, r, indexPath)
		}))
	}

	return r
}
