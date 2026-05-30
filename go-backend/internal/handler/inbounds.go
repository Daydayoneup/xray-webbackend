package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"xray-panel/internal/model"
)

func toModelAuth(a *model.AuthIn) *model.Auth {
	if a == nil {
		return nil
	}
	return &model.Auth{User: a.User, Password: a.Password}
}

func (s *Server) ListInbounds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Inbounds)
}

func (s *Server) CreateInbound(w http.ResponseWriter, r *http.Request) {
	var body model.InboundIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err)); return
	}
	if body.Port == s.App.Config().PanelPort {
		writeError(w, 400, fmt.Sprintf("入站端口 %d 与面板端口冲突", body.Port)); return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	for _, ib := range state.Inbounds {
		if ib.Port == body.Port {
			writeError(w, 400, fmt.Sprintf("入站端口 %d 重复", body.Port)); return
		}
	}

	tag := fmt.Sprintf("in-%d", state.InboundSeq)
	state.InboundSeq++
	listen := body.Listen
	if listen == "" {
		listen = "127.0.0.1"
	}
	ib := model.Inbound{
		Tag: tag, Protocol: body.Protocol, Listen: listen,
		Port: body.Port, UDP: body.UDP, Auth: toModelAuth(body.Auth),
	}
	state.Inbounds = append(state.Inbounds, ib)
	if err := s.App.Persist(); err != nil {
		writeError(w, 500, "保存失败"); return
	}
	writeJSON(w, 201, ib)
}

func (s *Server) UpdateInbound(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	var body model.InboundIn
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
	for i, ib := range state.Inbounds {
		if ib.Tag == tag {
			idx = i; break
		}
	}
	if idx < 0 {
		writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag)); return
	}
	for i, ib := range state.Inbounds {
		if i != idx && ib.Port == body.Port {
			writeError(w, 400, fmt.Sprintf("入站端口 %d 重复", body.Port)); return
		}
	}
	if body.Port == s.App.Config().PanelPort {
		writeError(w, 400, fmt.Sprintf("入站端口 %d 与面板端口冲突", body.Port)); return
	}
	listen := body.Listen
	if listen == "" {
		listen = "127.0.0.1"
	}
	ib := model.Inbound{
		Tag: tag, Protocol: body.Protocol, Listen: listen,
		Port: body.Port, UDP: body.UDP, Auth: toModelAuth(body.Auth),
	}
	state.Inbounds[idx] = ib
	if err := s.App.Persist(); err != nil {
		writeError(w, 500, "保存失败"); return
	}
	writeJSON(w, 200, ib)
}

func (s *Server) DeleteInbound(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	var kept []model.Inbound
	found := false
	for _, ib := range state.Inbounds {
		if ib.Tag == tag {
			found = true
		} else {
			kept = append(kept, ib)
		}
	}
	if !found {
		writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag)); return
	}
	state.Inbounds = kept
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
