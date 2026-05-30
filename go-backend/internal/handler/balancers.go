package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"xray-panel/internal/model"
)

func (s *Server) ListBalancers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Balancers)
}

func (s *Server) CreateBalancer(w http.ResponseWriter, r *http.Request) {
	var body model.BalancerIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err)); return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	nodeTags := map[string]bool{}
	for _, n := range state.Nodes {
		nodeTags[n.Tag] = true
	}
	var members []string
	for _, t := range body.Nodes {
		if nodeTags[t] {
			members = append(members, t)
		}
	}
	if len(members) == 0 {
		writeError(w, 400, fmt.Sprintf("自动组「%s」没有有效节点", body.Name)); return
	}
	tag := fmt.Sprintf("auto-%d", state.BalancerSeq)
	state.BalancerSeq++
	name := body.Name
	if name == "" {
		name = tag
	}
	bal := model.Balancer{Tag: tag, Name: name, Nodes: members, Strategy: "leastPing"}
	state.Balancers = append(state.Balancers, bal)
	s.App.Persist()
	writeJSON(w, 201, bal)
}

func (s *Server) UpdateBalancer(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	var body model.BalancerIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err)); return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	idx := -1
	for i, b := range state.Balancers {
		if b.Tag == tag {
			idx = i; break
		}
	}
	if idx < 0 {
		writeError(w, 404, fmt.Sprintf("自动组 %s 不存在", tag)); return
	}
	nodeTags := map[string]bool{}
	for _, n := range state.Nodes {
		nodeTags[n.Tag] = true
	}
	var members []string
	for _, t := range body.Nodes {
		if nodeTags[t] {
			members = append(members, t)
		}
	}
	if len(members) == 0 {
		writeError(w, 400, fmt.Sprintf("自动组「%s」没有有效节点", body.Name)); return
	}
	name := body.Name
	if name == "" {
		name = tag
	}
	bal := model.Balancer{Tag: tag, Name: name, Nodes: members, Strategy: "leastPing"}
	state.Balancers[idx] = bal
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
		writeError(w, 404, fmt.Sprintf("自动组 %s 不存在", tag)); return
	}
	state.Balancers = kept
	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
