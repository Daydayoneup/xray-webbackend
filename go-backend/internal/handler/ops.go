package handler

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/app"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// ---------- 规则测试 ----------

func (s *Server) TestRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
		Source string `json:"source"` // draft | applied
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	var cfg map[string]any
	var src []int
	if body.Source == "applied" {
		if cfg = s.App.AppliedConfig(); cfg == nil {
			writeError(w, 400, "尚未应用过配置")
			return
		}
	} else {
		s.App.Store().Lock()
		res := s.App.Build()
		s.App.Store().Unlock()
		cfg, src = res.Config, res.RuleSource
	}
	t, err := service.TestRoute(cfg, src, s.App.GeoDB(), body.Target, nil)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.App.Store().Lock()
	label := s.App.OutboundLabel(t.Outbound)
	// 规则/默认出口配置的是手动选择组时，显示“组 → 实际节点”
	configured := ""
	st := s.App.State()
	if t.UserRule > 0 && t.UserRule <= len(st.Rules) {
		configured = st.Rules[t.UserRule-1].Outbound
	} else if t.Kind == "default" && t.Matched {
		configured = st.DefaultOutbound
	}
	if configured != "" && configured != t.Outbound {
		for _, b := range st.Balancers {
			if b.Tag == configured {
				label = s.App.OutboundLabel(configured) + " → " + label
			}
		}
	}
	s.App.Store().Unlock()
	writeJSON(w, 200, map[string]any{"result": t, "label": label})
}

// ---------- 流量 ----------

func (s *Server) Traffic(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 90 {
		days = 7
	}
	writeJSON(w, 200, s.App.Traffic(days))
}

// ---------- 在线用户 ----------

func (s *Server) Online(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	limit := atoi("limit")
	if limit > 500 {
		limit = 500
	}
	writeJSON(w, 200, s.App.Online(app.OnlineQuery{
		Q: q.Get("q"), Inbound: q.Get("inbound"), Sort: q.Get("sort"),
		Offset: atoi("offset"), Limit: limit,
		EventKind: q.Get("event_kind"), EventOffset: atoi("event_offset"), EventLimit: min(atoi("event_limit"), 500),
	}))
}

// ---------- 配置历史 ----------

func (s *Server) ListHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.ListHistory())
}

func (s *Server) HistoryDiff(w http.ResponseWriter, r *http.Request) {
	lines, err := s.App.HistoryDiff(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"lines": lines})
}

func (s *Server) RestoreHistory(w http.ResponseWriter, r *http.Request) {
	res, err := s.App.RestoreHistory(chi.URLParam(r, "id"))
	if err != nil {
		s.writeApplyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "xray_running": res.Running, "warnings": nonNil(res.Warnings)})
}

// ---------- 备份 ----------

func (s *Server) ExportBackup(w http.ResponseWriter, r *http.Request) {
	data, err := s.App.Export()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="xray-panel-backup-%s.json"`, time.Now().Format("20060102-150405")))
	w.Write(data)
}

func (s *Server) ImportBackup(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		writeError(w, 400, "读取文件失败")
		return
	}
	summary, err := s.App.Import(data)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "summary": summary})
}

// ---------- 设置 ----------

func (s *Server) settingsView() map[string]any {
	st := s.App.State()
	sources := []map[string]string{}
	for id, src := range app.GeoSources {
		sources = append(sources, map[string]string{"id": id, "name": src.Name})
	}
	return map[string]any{
		"public_host":     st.PublicHost,
		"dns":             st.DNS,
		"dns_defaults":    map[string]any{"domestic": service.DefaultDomesticDNS, "remote": service.DefaultRemoteDNS},
		"notify":          st.Notify,
		"notify_events":   app.NotifyEvents,
		"geo":             st.Geo,
		"geo_sources":     sources,
		"metrics_enabled": s.App.Config().MetricsPort > 0,
	}
}

func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	info := s.App.GeoInfo()
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	v := s.settingsView()
	v["geo_files"] = info
	writeJSON(w, 200, v)
}

type settingsIn struct {
	PublicHost *string               `json:"public_host"`
	DNS        *model.DNSSettings    `json:"dns"`
	Notify     *model.NotifySettings `json:"notify"`
	Geo        *struct {
		Source         string `json:"source"`
		AutoUpdateDays int    `json:"auto_update_days"`
	} `json:"geo"`
}

func (s *Server) PutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if body.DNS != nil {
		ok := false
		for _, m := range service.DNSModes {
			ok = ok || m == body.DNS.Mode
		}
		if !ok {
			writeError(w, 400, "DNS 模式无效")
			return
		}
		if body.DNS.Mode == "custom" && len(body.DNS.Custom) == 0 {
			writeError(w, 400, "自定义模式至少需要一个 DNS 服务器")
			return
		}
	}
	if body.Notify != nil {
		n := body.Notify
		for _, p := range []*string{&n.BarkURL, &n.FeishuURL, &n.FeishuSecret, &n.DingTalkURL, &n.DingTalkSecret,
			&n.WeComURL, &n.WebhookURL, &n.TelegramToken, &n.TelegramChatID} {
			*p = strings.TrimSpace(*p)
		}
		if err := service.ValidateNotify(*n); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if body.Geo != nil {
		if _, ok := app.GeoSources[body.Geo.Source]; !ok {
			writeError(w, 400, "规则数据来源无效")
			return
		}
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	if body.PublicHost != nil {
		st.PublicHost = strings.TrimSpace(*body.PublicHost)
	}
	if body.DNS != nil {
		st.DNS = *body.DNS
	}
	if body.Notify != nil {
		st.Notify = *body.Notify
	}
	if body.Geo != nil {
		st.Geo.Source, st.Geo.AutoUpdateDays = body.Geo.Source, body.Geo.AutoUpdateDays
	}
	s.App.Persist()
	writeJSON(w, 200, s.settingsView())
}

func (s *Server) TestNotify(w http.ResponseWriter, r *http.Request) {
	if err := s.App.SendTestNotify(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) UpdateGeo(w http.ResponseWriter, r *http.Request) {
	info, err := s.App.UpdateGeo()
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, info)
}
