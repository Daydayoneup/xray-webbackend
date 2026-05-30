package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"xray-panel/internal/service"
)

func (s *Server) XrayStatus(w http.ResponseWriter, r *http.Request) {
	applied := readApplied(s.App.Config().ConfigPath)
	writeJSON(w, 200, map[string]any{
		"running": s.App.Xray().Running(),
		"applied": applied != nil,
		"dirty":   s.App.Dirty(applied),
	})
}

func (s *Server) Apply(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	if len(s.App.State().Nodes) == 0 {
		writeError(w, 400, "还没有节点,先拉取订阅"); return
	}
	cfg := service.BuildConfig(s.App.State())
	ok, out := s.App.Xray().TestConfig(cfgJSON(cfg))
	if !ok {
		if len(out) > 800 {
			out = out[len(out)-800:]
		}
		writeError(w, 400, fmt.Sprintf("配置校验失败: %s", out)); return
	}
	path := s.App.Config().ConfigPath
	data, _ := json.MarshalIndent(cfg, "", "  ")
	tmp := path + ".tmp"
	os.WriteFile(tmp, data, 0644)
	os.Rename(tmp, path)
	running := s.App.Xray().Restart(path)
	writeJSON(w, 200, map[string]any{"ok": true, "xray_running": running})
}

func (s *Server) XrayRestart(w http.ResponseWriter, r *http.Request) {
	ok := s.App.Xray().Restart(s.App.Config().ConfigPath)
	writeJSON(w, 200, map[string]any{"ok": ok, "xray_running": s.App.Xray().Running()})
}

func (s *Server) RawConfig(w http.ResponseWriter, r *http.Request) {
	applied := readApplied(s.App.Config().ConfigPath)
	if applied == nil {
		writeError(w, 404, "尚未应用过配置"); return
	}
	writeJSON(w, 200, applied)
}

func (s *Server) Topology(w http.ResponseWriter, r *http.Request) {
	cfg := readApplied(s.App.Config().ConfigPath)
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
		for i, r := range arr(routing["rules"]) {
			rm := asMap(r)
			desc := service.Describe(rm)
			tag := ""
			if t, _ := rm["balancerTag"].(string); t != "" {
				tag = t
			} else {
				tag, _ = rm["outboundTag"].(string)
			}
			rev[tag] = append(rev[tag], desc)
			route = append(route, map[string]any{
				"order": i + 1, "match": desc, "outbound": tag,
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
	applied := cfg != nil
	writeJSON(w, 200, map[string]any{
		"applied": applied, "dirty": s.App.Dirty(cfg),
		"inbounds": inbs, "outbounds": outs, "routing": route,
	})
}

func readApplied(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	return m
}

func cfgJSON(cfg map[string]any) []byte {
	data, _ := json.Marshal(cfg)
	return data
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
