package handler

import (
	"net/http"
	"strings"

	"xray-panel/internal/auth"
	"xray-panel/internal/model"
)

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body model.LoginIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	s.App.CheckPasswordReset()
	if !auth.VerifyPassword(s.App.State().Password, body.Password) {
		writeError(w, 401, "密码错误")
		return
	}
	writeJSON(w, 200, map[string]any{
		"token":      s.App.Sessions().Create(),
		"expires_in": 7 * 86400,
	})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		s.App.Sessions().Revoke(h[7:])
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": true, "password_initial": s.App.PasswordInitial()})
}

func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var body model.PasswordChangeIn
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
	if !auth.VerifyPassword(s.App.State().Password, body.OldPassword) {
		writeError(w, 400, "原密码错误")
		return
	}
	if err := s.App.SetPassword(body.NewPassword); err != nil {
		writeError(w, 500, "保存失败: "+err.Error())
		return
	}
	s.App.Sessions().Clear()
	writeJSON(w, 200, map[string]any{
		"ok":    true,
		"token": s.App.Sessions().Create(),
	})
}
