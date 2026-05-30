package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"xray-panel/internal/model"
)

func (s *Server) ListProxies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Proxies)
}

func (s *Server) CreateProxy(w http.ResponseWriter, r *http.Request) {
	var body model.ProxyIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err)); return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	tag := fmt.Sprintf("px-%d", state.ProxySeq)
	state.ProxySeq++
	name := body.Name
	if name == "" {
		name = tag
	}
	px := model.Proxy{
		Tag: tag, Name: name, Protocol: body.Protocol,
		Host: body.Host, Port: body.Port, Auth: toModelAuth(body.Auth),
	}
	state.Proxies = append(state.Proxies, px)
	s.App.Persist()
	writeJSON(w, 201, px)
}

func (s *Server) UpdateProxy(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	var body model.ProxyIn
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
	for i, p := range state.Proxies {
		if p.Tag == tag {
			idx = i; break
		}
	}
	if idx < 0 {
		writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag)); return
	}
	name := body.Name
	if name == "" {
		name = tag
	}
	px := model.Proxy{
		Tag: tag, Name: name, Protocol: body.Protocol,
		Host: body.Host, Port: body.Port, Auth: toModelAuth(body.Auth),
	}
	state.Proxies[idx] = px
	s.App.Persist()
	writeJSON(w, 200, px)
}

func (s *Server) DeleteProxy(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	var kept []model.Proxy
	found := false
	for _, p := range state.Proxies {
		if p.Tag == tag {
			found = true
		} else {
			kept = append(kept, p)
		}
	}
	if !found {
		writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag)); return
	}
	state.Proxies = kept
	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
