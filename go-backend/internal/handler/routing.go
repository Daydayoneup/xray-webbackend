package handler

import (
	"fmt"
	"net/http"
	"strings"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func (s *Server) routingView() map[string]any {
	st := s.App.State()
	rules := st.Rules
	if rules == nil { // 全新安装时为 nil，返回 [] 避免前端出错
		rules = []model.Rule{}
	}
	return map[string]any{
		"default_outbound": st.DefaultOutbound,
		"domain_strategy":  selOr(st.DomainStrategy, service.DefaultDomainStrategy),
		"rules":            rules,
	}
}

func (s *Server) GetRouting(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.routingView())
}

func (s *Server) PutRouting(w http.ResponseWriter, r *http.Request) {
	var body model.RoutingIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	valid := s.App.OutboundTags()
	if body.DefaultOutbound != "" && !valid[body.DefaultOutbound] {
		writeError(w, 400, fmt.Sprintf("默认出口 %s 不存在", body.DefaultOutbound))
		return
	}
	rules := []model.Rule{}
	for i, r := range body.Rules {
		if !valid[r.Outbound] {
			writeError(w, 400, fmt.Sprintf("规则 #%d 的出口 %s 不存在", i+1, r.Outbound))
			return
		}
		if err := service.ValidateRuleValue(r.Type, r.Value); err != nil {
			writeError(w, 400, fmt.Sprintf("规则 #%d: %v", i+1, err))
			return
		}
		if r.Type == "set" && findSet(s.App.State(), strings.TrimSpace(r.Value)) < 0 {
			writeError(w, 400, fmt.Sprintf("规则 #%d 引用的分组不存在", i+1))
			return
		}
		rules = append(rules, model.Rule{
			ID: i + 1, Name: strings.TrimSpace(r.Name), Type: r.Type, Value: r.Value,
			Outbound: r.Outbound, Enabled: r.Enabled,
		})
	}
	st := s.App.State()
	st.DefaultOutbound = body.DefaultOutbound
	if body.DomainStrategy != "" {
		st.DomainStrategy = body.DomainStrategy
	}
	st.Rules = rules
	s.App.Persist()
	writeJSON(w, 200, s.routingView())
}

func (s *Server) Templates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, service.Templates)
}

func (s *Server) Outbounds(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.App.Outbounds())
}
