package handler

import (
	"fmt"
	"net/http"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func (s *Server) GetRouting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"default_outbound": s.App.State().DefaultOutbound,
		"rules":            s.App.State().Rules,
	})
}

func (s *Server) PutRouting(w http.ResponseWriter, r *http.Request) {
	var body model.RoutingIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	valid := s.App.OutboundTags()
	if body.DefaultOutbound != "" && !valid[body.DefaultOutbound] {
		writeError(w, 400, fmt.Sprintf("默认出口 %s 不存在", body.DefaultOutbound)); return
	}
	var rules []model.Rule
	for i, r := range body.Rules {
		if !valid[r.Outbound] {
			writeError(w, 400, fmt.Sprintf("规则出口 %s 不存在", r.Outbound)); return
		}
		rules = append(rules, model.Rule{
			ID: i + 1, Type: r.Type, Value: r.Value,
			Outbound: r.Outbound, Enabled: r.Enabled,
		})
	}
	s.App.State().DefaultOutbound = body.DefaultOutbound
	s.App.State().Rules = rules
	s.App.Persist()
	writeJSON(w, 200, map[string]any{
		"default_outbound": s.App.State().DefaultOutbound,
		"rules":            s.App.State().Rules,
	})
}

func (s *Server) Templates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, service.Templates)
}

func (s *Server) Outbounds(w http.ResponseWriter, r *http.Request) {
	var out []map[string]string
	for _, n := range s.App.State().Nodes {
		out = append(out, map[string]string{"tag": n.Tag, "label": n.Name, "kind": "node"})
	}
	for _, b := range s.App.State().Balancers {
		out = append(out, map[string]string{
			"tag": b.Tag, "label": "⚖ " + b.Name + "(自动)", "kind": "balancer",
		})
	}
	for _, p := range s.App.State().Proxies {
		out = append(out, map[string]string{
			"tag": p.Tag, "label": "🛰 " + p.Name + "(落地)", "kind": "proxy",
		})
	}
	out = append(out,
		map[string]string{"tag": "direct", "label": "直连", "kind": "builtin"},
		map[string]string{"tag": "block", "label": "阻断", "kind": "builtin"},
	)
	writeJSON(w, 200, out)
}
