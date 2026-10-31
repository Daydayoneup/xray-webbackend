package handler

import (
	"errors"
	"net/http"
	"strconv"

	"xray-panel/internal/app"
	"xray-panel/internal/service"
)

func (s *Server) XrayStatus(w http.ResponseWriter, r *http.Request) {
	applied := s.App.AppliedConfig()
	ps := s.App.Xray().Status()
	s.App.Store().Lock()
	res := s.App.Build()
	dirty := s.App.Dirty(applied)
	s.App.Store().Unlock()
	writeJSON(w, 200, map[string]any{
		"running":    ps.Running,
		"state":      ps.State,
		"version":    ps.Version,
		"restarts":   ps.Restarts,
		"last_exit":  ps.LastExit,
		"started_at": ps.StartedAt,
		"applied":    applied != nil,
		"dirty":      dirty,
		"warnings":   nonNil(res.Warnings),
		"install":    s.App.XrayInstall(),
		// 仍在使用自动生成的初始密码时，前端提醒修改
		"password_initial": s.App.PasswordInitial(),
		"panel_version":    s.App.Config().Version,
	})
}

// InstallXray 在后台重新尝试自动安装 Xray（首次启动自动安装失败后，从顶栏重试）。
func (s *Server) InstallXray(w http.ResponseWriter, r *http.Request) {
	if s.App.Xray().Running() {
		writeError(w, 400, "Xray 已在运行")
		return
	}
	if st := s.App.XrayInstall(); st.State == "installing" {
		writeJSON(w, 200, st)
		return
	}
	go s.App.InstallXray()
	writeJSON(w, 200, app.XrayInstallStatus{State: "installing"})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Server) Apply(w http.ResponseWriter, r *http.Request) {
	res, err := s.App.Apply()
	if err != nil {
		s.writeApplyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "xray_running": res.Running, "warnings": nonNil(res.Warnings)})
}

func (s *Server) writeApplyError(w http.ResponseWriter, err error) {
	var ae *app.ApplyError
	if errors.As(err, &ae) {
		code := 500
		if ae.Validation {
			code = 400
		}
		writeJSON(w, code, ae)
		return
	}
	writeError(w, 400, err.Error())
}

func (s *Server) XrayRestart(w http.ResponseWriter, r *http.Request) {
	if s.App.AppliedConfig() == nil {
		writeError(w, 400, "尚未应用过配置，请先点击「应用」")
		return
	}
	if !s.App.Xray().Restart(s.App.Config().ConfigPath) {
		writeJSON(w, 500, map[string]any{"detail": "Xray 重启后未能保持运行", "logs": s.App.Xray().Logs(30)})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "xray_running": true})
}

func (s *Server) XrayLogs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 {
		n = 200
	}
	writeJSON(w, 200, map[string]any{"lines": s.App.Xray().Logs(n)})
}

func (s *Server) RawConfig(w http.ResponseWriter, r *http.Request) {
	applied := s.App.AppliedConfig()
	if applied == nil {
		writeError(w, 404, "尚未应用过配置")
		return
	}
	writeJSON(w, 200, applied)
}

func (s *Server) DraftConfig(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	res := s.App.Build()
	s.App.Store().Unlock()
	writeJSON(w, 200, map[string]any{"config": res.Config, "warnings": nonNil(res.Warnings)})
}

func (s *Server) DraftDiff(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"lines": s.App.DraftDiff()})
}

func (s *Server) Topology(w http.ResponseWriter, r *http.Request) {
	cfg := s.App.AppliedConfig()
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	inbs, outs, route := []any{}, []any{}, []any{}
	if cfg != nil {
		for _, ib := range arr(cfg["inbounds"]) {
			m := asMap(ib)
			inbs = append(inbs, map[string]any{
				"tag": m["tag"], "protocol": m["protocol"],
				"listen": m["listen"], "port": m["port"],
			})
		}
		rev := map[string][]string{}
		routing := asMap(cfg["routing"])
		order := 0
		for _, r := range arr(routing["rules"]) {
			rm := asMap(r)
			if _, internal := rm["inboundTag"]; internal {
				continue // metrics / DNS 内部规则不展示
			}
			order++
			desc := service.Describe(rm)
			tag := ""
			if t, _ := rm["balancerTag"].(string); t != "" {
				tag = t
			} else {
				tag, _ = rm["outboundTag"].(string)
			}
			rev[tag] = append(rev[tag], desc)
			route = append(route, map[string]any{
				"order": order, "match": desc, "outbound": tag,
				"label": s.App.OutboundLabel(tag),
			})
		}
		for _, ob := range arr(cfg["outbounds"]) {
			om := asMap(ob)
			t, _ := om["tag"].(string)
			outs = append(outs, map[string]any{
				"tag": t, "protocol": om["protocol"],
				"label": s.App.OutboundLabel(t),
				"rules": rev[t],
			})
		}
	}
	writeJSON(w, 200, map[string]any{
		"applied": cfg != nil, "dirty": s.App.Dirty(cfg),
		"inbounds": inbs, "outbounds": outs, "routing": route,
	})
}

func arr(v any) []any {
	a, _ := v.([]any)
	if a == nil {
		return []any{}
	}
	return a
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
