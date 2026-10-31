package handler

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// inboundView 是返回给前端的入站：users 为关联的用户名。
type inboundView struct {
	model.Inbound
	Users       []string `json:"users"`
	ActiveUsers int      `json:"active_users"`
}

func (s *Server) ListInbounds(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	out := []inboundView{}
	for i := range st.Inbounds {
		out = append(out, newInboundView(st, &st.Inbounds[i]))
	}
	writeJSON(w, 200, out)
}

func newInboundView(st *model.PanelState, ib *model.Inbound) inboundView {
	v := inboundView{Inbound: *ib, Users: []string{}}
	for _, u := range service.InboundUsers(st, ib) {
		v.Users = append(v.Users, u.Name)
		if !u.Disabled {
			v.ActiveUsers++
		}
	}
	return v
}

var ss2022Methods = map[string]bool{"2022-blake3-aes-128-gcm": true, "2022-blake3-aes-256-gcm": true, "2022-blake3-chacha20-poly1305": true}

// buildInbound 把请求转换为入站，补全/生成缺省的密钥。用户关联由调用方处理。
func buildInbound(body *model.InboundIn, tag string, state *model.PanelState) (*model.Inbound, error) {
	ib := &model.Inbound{Tag: tag, Protocol: body.Protocol, Remark: strings.TrimSpace(body.Remark),
		Listen: strings.TrimSpace(body.Listen), Port: body.Port, UDP: body.UDP}
	if ib.Listen == "" {
		ib.Listen = "127.0.0.1"
	}
	switch ib.Protocol {
	case "shadowsocks":
		ib.SSMethod = selOr(strings.TrimSpace(body.SSMethod), "2022-blake3-aes-128-gcm")
		if !ss2022Methods[ib.SSMethod] {
			return nil, fmt.Errorf("多用户 Shadowsocks 只支持 2022 系列加密方式")
		}
		ib.SSKey = strings.TrimSpace(body.SSKey)
		if ib.SSKey == "" {
			ib.SSKey = service.NewSSKey(ib.SSMethod)
		} else if !service.ValidSSKey(ib.SSMethod, ib.SSKey) {
			return nil, fmt.Errorf("服务端密钥长度与加密方式不匹配（%s 需要 %d 字节 base64）", ib.SSMethod, service.SSKeyLen(ib.SSMethod))
		}
	case "vless":
		if err := fillReality(ib, body); err != nil {
			return nil, err
		}
	}
	if d := body.DNS; d != nil && strings.TrimSpace(d.Server) != "" {
		addr, err := service.NormalizeDNSServer(d.Server)
		if err != nil {
			return nil, fmt.Errorf("DNS 服务器：%v", err)
		}
		via := strings.TrimSpace(d.Via)
		if via == "direct" {
			via = ""
		}
		if via != "" && !isNodeOrProxy(state, via) {
			return nil, fmt.Errorf("DNS 出口只能是直连、订阅节点或自定义代理")
		}
		ib.DNS = &model.InboundDNS{Server: addr, Via: via}
	}
	return ib, nil
}

func isNodeOrProxy(state *model.PanelState, tag string) bool {
	for _, n := range state.Nodes {
		if n.Tag == tag {
			return true
		}
	}
	for _, p := range state.Proxies {
		if p.Tag == tag {
			return true
		}
	}
	return false
}

// linkInboundUsers 让入站恰好关联 names 中的用户。调用方持锁。
func linkInboundUsers(state *model.PanelState, tag string, names []string) error {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.TrimSpace(n)] = true
	}
	found := 0
	for i := range state.Users {
		if want[state.Users[i].Name] {
			found++
		}
	}
	if found != len(want) {
		return fmt.Errorf("关联的用户不存在，请刷新页面后重试")
	}
	for i := range state.Users {
		u := &state.Users[i]
		has := containsTag(u.Inbounds, tag)
		switch {
		case want[u.Name] && !has:
			u.Inbounds = append(u.Inbounds, tag)
		case !want[u.Name] && has:
			u.Inbounds = removeTag(u.Inbounds, tag)
		}
	}
	return nil
}

func containsTag(xs []string, t string) bool {
	for _, x := range xs {
		if x == t {
			return true
		}
	}
	return false
}

func removeTag(xs []string, t string) []string {
	out := []string{}
	for _, x := range xs {
		if x != t {
			out = append(out, x)
		}
	}
	return out
}

// ensureUser 返回同名用户，不存在则创建（凭据随机生成）。调用方持锁。
func ensureUser(state *model.PanelState, name string) *model.User {
	for i := range state.Users {
		if state.Users[i].Name == name {
			return &state.Users[i]
		}
	}
	u := model.User{Name: name, CreatedAt: time.Now().Unix()}
	service.NewUserCredentials(&u)
	state.Users = append(state.Users, u)
	return &state.Users[len(state.Users)-1]
}

func fillReality(ib *model.Inbound, body *model.InboundIn) error {
	r := &model.RealityConf{Dest: strings.TrimSpace(body.RealityDest), PrivateKey: strings.TrimSpace(body.RealityPrivateKey)}
	if r.Dest == "" {
		r.Dest = "www.microsoft.com:443"
	}
	host, _, err := net.SplitHostPort(r.Dest)
	if err != nil {
		return fmt.Errorf("伪装目标格式应为 域名:端口，例如 www.microsoft.com:443")
	}
	for _, n := range body.RealityServerNames {
		if n = strings.TrimSpace(n); n != "" {
			r.ServerNames = append(r.ServerNames, n)
		}
	}
	if len(r.ServerNames) == 0 {
		r.ServerNames = []string{host}
	}
	if r.PrivateKey == "" {
		r.PrivateKey, r.PublicKey = service.NewRealityKeys()
	} else if r.PublicKey, err = service.RealityPublicKey(r.PrivateKey); err != nil {
		return err
	}
	for _, id := range body.RealityShortIDs {
		if id = strings.TrimSpace(id); id != "" {
			r.ShortIDs = append(r.ShortIDs, id)
		}
	}
	if len(r.ShortIDs) == 0 {
		r.ShortIDs = []string{service.NewShortID()}
	}
	ib.Reality = r
	return nil
}

func (s *Server) saveInbound(w http.ResponseWriter, r *http.Request, tag string) {
	var body model.InboundIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	cfg := s.App.Config()
	if body.Port == cfg.PanelPort {
		writeError(w, 400, fmt.Sprintf("端口 %d 与面板端口冲突", body.Port))
		return
	}
	if cfg.MetricsPort > 0 && body.Port == cfg.MetricsPort {
		writeError(w, 400, fmt.Sprintf("端口 %d 已被流量统计使用（METRICS_PORT）", body.Port))
		return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	state := s.App.State()
	idx := -1
	isNew := tag == ""
	if isNew {
		seq := state.InboundSeq
		tag = nextTag("in-", &seq, func(t string) bool {
			for _, ib := range state.Inbounds {
				if ib.Tag == t {
					return true
				}
			}
			return false
		})
		state.InboundSeq = seq
	} else {
		for i, ib := range state.Inbounds {
			if ib.Tag == tag {
				idx = i
			}
		}
		if idx < 0 {
			writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag))
			return
		}
	}
	for i, ib := range state.Inbounds {
		if i != idx && ib.Port == body.Port {
			writeError(w, 400, fmt.Sprintf("端口 %d 已被其他入站使用", body.Port))
			return
		}
	}
	ib, err := buildInbound(&body, tag, state)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	names := body.Users
	if a := body.Auth; a != nil && a.User != "" && len(names) == 0 { // 兼容旧客户端：单账号
		if err := service.ValidateUserName(a.User); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		u := ensureUser(state, a.User)
		if a.Password != "" {
			u.Password = a.Password
		}
		names = []string{a.User}
	}
	if isNew && len(names) == 0 && len(state.Users) == 0 && (ib.Protocol == "vless" || ib.Protocol == "shadowsocks") {
		names = []string{ensureUser(state, "user1").Name} // 加密入站至少需要一个用户
	}
	if body.Users != nil || len(names) > 0 || isNew {
		if err := linkInboundUsers(state, tag, names); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if isNew {
		state.Inbounds = append(state.Inbounds, *ib)
	} else {
		state.Inbounds[idx] = *ib
	}
	if err := s.App.Persist(); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	code := 200
	if idx < 0 {
		code = 201
	}
	writeJSON(w, code, newInboundView(state, ib))
}

func (s *Server) CreateInbound(w http.ResponseWriter, r *http.Request) { s.saveInbound(w, r, "") }
func (s *Server) UpdateInbound(w http.ResponseWriter, r *http.Request) {
	s.saveInbound(w, r, chi.URLParam(r, "tag"))
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
		writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag))
		return
	}
	state.Inbounds = kept
	for i := range state.Users {
		state.Users[i].Inbounds = removeTag(state.Users[i].Inbounds, tag)
	}
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ShareInbound 生成每个用户的分享链接。host 为客户端连接用的服务器地址。
func (s *Server) ShareInbound(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	if host == "" {
		host = s.App.State().PublicHost
	}
	if host == "" {
		writeError(w, 400, "请先填写服务器地址（客户端连接用的 IP 或域名）")
		return
	}
	for i := range s.App.State().Inbounds {
		ib := &s.App.State().Inbounds[i]
		if ib.Tag != tag {
			continue
		}
		type item struct {
			Name     string `json:"name"`
			Link     string `json:"link"`
			Disabled bool   `json:"disabled"`
		}
		out := []item{}
		users := service.InboundUsers(s.App.State(), ib)
		if len(users) == 0 && (ib.Protocol == "socks" || ib.Protocol == "http") {
			users = []model.InboundUser{{}}
		}
		for _, u := range users {
			out = append(out, item{Name: selOr(u.Name, "（无账号）"), Link: service.ShareLink(ib, u, host), Disabled: u.Disabled})
		}
		writeJSON(w, 200, map[string]any{"host": host, "items": out})
		return
	}
	writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag))
}

// GenSecret 生成 UUID / reality 密钥 / ss2022 密钥，供前端“随机生成”按钮使用。
func (s *Server) GenSecret(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("type") {
	case "uuid":
		writeJSON(w, 200, map[string]string{"value": service.NewUUID()})
	case "reality":
		priv, pub := service.NewRealityKeys()
		writeJSON(w, 200, map[string]string{"private_key": priv, "public_key": pub, "short_id": service.NewShortID()})
	case "sskey":
		writeJSON(w, 200, map[string]string{"value": service.NewSSKey(r.URL.Query().Get("method"))})
	default:
		writeError(w, 400, "type 只能是 uuid / reality / sskey")
	}
}
