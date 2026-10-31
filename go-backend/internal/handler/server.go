package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"xray-panel/internal/app"
	"xray-panel/internal/middleware"
	"xray-panel/internal/webui"
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
		r.Post("/api/xray/install", s.InstallXray)
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

	s.mountWeb(r)
	return r
}

// mountWeb 提供前端页面：/assets/ 为带哈希的构建产物（长期缓存），其余非 API 路径返回 index.html（SPA）。
func (s *Server) mountWeb(r chi.Router) {
	web, embedded := webui.FS("frontend_dist")
	if web == nil {
		slog.Warn("没有找到前端文件（未嵌入，也没有 frontend_dist 目录），只提供 API")
		return
	}
	if !embedded {
		slog.Info("使用 frontend_dist 目录中的前端文件")
	}
	files := http.FileServerFS(web)
	r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := strings.TrimPrefix(path.Clean(req.URL.Path), "/")
		if strings.HasPrefix(p, "api/") {
			writeError(w, 404, "not found")
			return
		}
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(web, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, req)
				return
			}
			if strings.HasPrefix(p, "assets/") {
				http.NotFound(w, req)
				return
			}
		}
		// 前端路由与 index.html：不缓存，升级后立即加载新版本
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, req, web, "index.html")
	}))
}
