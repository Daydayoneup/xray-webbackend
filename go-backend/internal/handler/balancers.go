package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// GroupStatus 返回各节点组的成员与当前使用情况。
func (s *Server) GroupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.GroupStatuses())
}

// PreviewGroup 预览关键字会收纳哪些节点。
func (s *Server) PreviewGroup(w http.ResponseWriter, r *http.Request) {
	var body model.BalancerIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	b := &model.Balancer{Nodes: body.Nodes, Filter: body.Filter, FilterSubID: body.FilterSubID}
	out := []map[string]string{}
	for _, t := range service.GroupCandidates(s.App.State(), b) {
		out = append(out, map[string]string{"tag": t, "label": s.App.OutboundLabel(t)})
	}
	writeJSON(w, 200, out)
}

// SelectGroup 切换手动选择组的节点并立即应用。
func (s *Server) SelectGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Selected string `json:"selected"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	res, err := s.App.SelectGroupMember(chi.URLParam(r, "tag"), body.Selected)
	if err != nil {
		s.writeApplyError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "xray_running": res.Running, "warnings": nonNil(res.Warnings)})
}

func (s *Server) ListBalancers(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.App.State().Balancers)
}

// balancerFromBody 校验成员（可以是节点或自定义代理）。调用方持锁。
func (s *Server) balancerFromBody(body model.BalancerIn, tag string) (*model.Balancer, error) {
	valid := map[string]bool{}
	for _, n := range s.App.State().Nodes {
		valid[n.Tag] = true
	}
	for _, p := range s.App.State().Proxies {
		valid[p.Tag] = true
	}
	var members []string
	seen := map[string]bool{}
	for _, t := range body.Nodes {
		if valid[t] && !seen[t] {
			seen[t] = true
			members = append(members, t)
		}
	}
	filter := strings.TrimSpace(body.Filter)
	if len(members) == 0 && filter == "" {
		return nil, fmt.Errorf("请至少选择一个成员，或填写自动收纳的关键字")
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = tag
	}
	strategy := body.Strategy
	if strategy == "" {
		strategy = "leastPing"
	}
	b := &model.Balancer{Tag: tag, Name: name, Nodes: members, Filter: filter, FilterSubID: body.FilterSubID,
		Strategy: strategy, Selected: body.Selected}
	if len(service.GroupCandidates(s.App.State(), b)) == 0 {
		return nil, fmt.Errorf("关键字「%s」没有匹配到任何节点", filter)
	}
	return b, nil
}

func (s *Server) CreateBalancer(w http.ResponseWriter, r *http.Request) {
	var body model.BalancerIn
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

	state := s.App.State()
	seq := state.BalancerSeq
	tag := nextTag("auto-", &seq, func(t string) bool { return s.App.OutboundTags()[t] })
	bal, err := s.balancerFromBody(body, tag)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	state.BalancerSeq = seq
	state.Balancers = append(state.Balancers, *bal)
	s.App.Persist()
	writeJSON(w, 201, bal)
}

func (s *Server) UpdateBalancer(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	var body model.BalancerIn
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

	state := s.App.State()
	idx := -1
	for i, b := range state.Balancers {
		if b.Tag == tag {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeError(w, 404, fmt.Sprintf("节点组 %s 不存在", tag))
		return
	}
	bal, err := s.balancerFromBody(body, tag)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	state.Balancers[idx] = *bal
	s.App.Persist()
	writeJSON(w, 200, bal)
}

func (s *Server) DeleteBalancer(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	var kept []model.Balancer
	found := false
	for _, b := range state.Balancers {
		if b.Tag == tag {
			found = true
		} else {
			kept = append(kept, b)
		}
	}
	if !found {
		writeError(w, 404, fmt.Sprintf("节点组 %s 不存在", tag))
		return
	}
	state.Balancers = kept
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
