package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.UserViews())
}

func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) { s.saveUser(w, r, "") }
func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request) {
	s.saveUser(w, r, chi.URLParam(r, "name"))
}

// saveUser 新建（name 为空）或修改用户。用户名创建后不可修改（它是流量统计与在线识别的 key）。
func (s *Server) saveUser(w http.ResponseWriter, r *http.Request, name string) {
	var body model.UserIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	u := model.User{Name: strings.TrimSpace(body.Name), Remark: strings.TrimSpace(body.Remark), Disabled: body.Disabled,
		UUID: strings.TrimSpace(body.UUID), Password: strings.TrimSpace(body.Password), SSKey: strings.TrimSpace(body.SSKey)}
	if name != "" {
		u.Name = name
	}
	if err := service.ValidateUserName(u.Name); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if u.UUID != "" && !service.IsUUID(u.UUID) {
		writeError(w, 400, "UUID 格式错误")
		return
	}
	if strings.ContainsAny(u.Password, " :@") {
		writeError(w, 400, "密码不能包含空格、: 或 @")
		return
	}
	if u.SSKey != "" && !service.ValidUserSSKey(u.SSKey) {
		writeError(w, 400, "Shadowsocks 密钥应为 16 或 32 字节的 base64")
		return
	}
	service.NewUserCredentials(&u)

	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	tags := map[string]bool{}
	for _, ib := range st.Inbounds {
		tags[ib.Tag] = true
	}
	u.Inbounds = []string{}
	for _, t := range body.Inbounds {
		if !tags[t] {
			writeError(w, 400, fmt.Sprintf("入站 %s 不存在", t))
			return
		}
		if !containsTag(u.Inbounds, t) {
			u.Inbounds = append(u.Inbounds, t)
		}
	}
	idx := -1
	for i := range st.Users {
		if st.Users[i].Name == u.Name {
			idx = i
		}
	}
	code := 200
	switch {
	case name == "" && idx >= 0:
		writeError(w, 400, fmt.Sprintf("用户 %s 已存在", u.Name))
		return
	case name == "":
		u.CreatedAt = time.Now().Unix()
		st.Users = append(st.Users, u)
		code = 201
	case idx < 0:
		writeError(w, 404, fmt.Sprintf("用户 %s 不存在", name))
		return
	default:
		u.CreatedAt = st.Users[idx].CreatedAt
		st.Users[idx] = u
	}
	if err := s.App.Persist(); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, code, u)
}

func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	kept := []model.User{}
	for _, u := range st.Users {
		if u.Name != name {
			kept = append(kept, u)
		}
	}
	if len(kept) == len(st.Users) {
		writeError(w, 404, fmt.Sprintf("用户 %s 不存在", name))
		return
	}
	st.Users = kept
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ShareUser 返回该用户在每个关联入站的分享链接。
func (s *Server) ShareUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	if host == "" {
		host = st.PublicHost
	}
	if host == "" {
		writeError(w, 400, "请先填写服务器地址（客户端连接用的 IP 或域名）")
		return
	}
	for i := range st.Users {
		u := &st.Users[i]
		if u.Name != name {
			continue
		}
		type item struct {
			Name     string `json:"name"`
			Link     string `json:"link"`
			Disabled bool   `json:"disabled"`
			Local    bool   `json:"local"` // 入站只监听本机
		}
		out := []item{}
		for j := range st.Inbounds {
			ib := &st.Inbounds[j]
			if !containsTag(u.Inbounds, ib.Tag) {
				continue
			}
			iu := model.InboundUser{Name: u.Name, Secret: service.UserSecret(u, ib), Disabled: u.Disabled}
			out = append(out, item{Name: fmt.Sprintf("%s :%d", selOr(ib.Remark, ib.Protocol), ib.Port),
				Link: service.ShareLink(ib, iu, host), Disabled: u.Disabled, Local: ib.Listen == "127.0.0.1" || ib.Listen == "::1"})
		}
		writeJSON(w, 200, map[string]any{"host": host, "items": out})
		return
	}
	writeError(w, 404, fmt.Sprintf("用户 %s 不存在", name))
}
