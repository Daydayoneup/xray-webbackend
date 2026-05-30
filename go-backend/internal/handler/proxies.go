package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
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

	px, err := buildProxy(&body)
	if err != nil {
		writeError(w, 400, err.Error()); return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	tag := fmt.Sprintf("px-%d", state.ProxySeq)
	state.ProxySeq++
	px.Tag = tag
	if px.Name == "" {
		px.Name = tag
	}
	state.Proxies = append(state.Proxies, *px)
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

	px, err := buildProxy(&body)
	if err != nil {
		writeError(w, 400, err.Error()); return
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
	px.Tag = tag
	if px.Name == "" {
		px.Name = tag
	}
	state.Proxies[idx] = *px
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

// buildProxy converts ProxyIn → Proxy, parsing share links for complex protocols
func buildProxy(body *model.ProxyIn) (*model.Proxy, error) {
	px := &model.Proxy{
		Protocol: body.Protocol,
		Host:     strings.TrimSpace(body.Host),
		Port:     body.Port,
		Auth:     toModelAuth(body.Auth),
		Link:     strings.TrimSpace(body.Link),
	}

	// 如果提供了 share link，解析完整 outbound
	if px.Link != "" {
		links, _ := service.ExtractLinks(px.Link)
		if len(links) == 0 {
			return nil, fmt.Errorf("无法解析分享链接")
		}
		nodes, skipped := service.ParseLinks(links)
		if len(nodes) == 0 {
			if len(skipped) > 0 {
				return nil, fmt.Errorf("链接解析失败: %s", skipped[0].Detail)
			}
			return nil, fmt.Errorf("未识别到有效协议")
		}
		n := nodes[0]
		px.Protocol = n.Type
		px.Host = n.Host
		px.Port = n.Port
		px.Name = n.Name
		px.RawOutbound = n.Outbound
		return px, nil
	}

	// socks/http: 需要 host
	if px.Protocol == "socks" || px.Protocol == "http" {
		if px.Host == "" {
			return nil, fmt.Errorf("代理地址(host)不能为空")
		}
		if px.Port == 0 {
			return nil, fmt.Errorf("端口不能为0")
		}
		return px, nil
	}

	// vmess/vless/trojan/shadowsocks 但没有提供 link
	return nil, fmt.Errorf("请粘贴 %s 分享链接自动解析", px.Protocol)
}
