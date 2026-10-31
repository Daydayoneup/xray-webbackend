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
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.App.State().Proxies)
}

func (s *Server) saveProxy(w http.ResponseWriter, r *http.Request, tag string) {
	var body model.ProxyIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	px, err := buildProxy(&body)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	state := s.App.State()
	idx := -1
	isNew := tag == ""
	if isNew {
		tag = nextTag("px-", &state.ProxySeq, func(t string) bool { return s.App.OutboundTags()[t] })
	} else {
		for i, p := range state.Proxies {
			if p.Tag == tag {
				idx = i
			}
		}
		if idx < 0 {
			writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag))
			return
		}
	}
	if err := s.App.CheckVia(tag, px.Via); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	px.Tag = tag
	if px.Name == "" {
		px.Name = tag
	}
	if isNew {
		state.Proxies = append(state.Proxies, *px)
		s.App.Persist()
		writeJSON(w, 201, px)
		return
	}
	state.Proxies[idx] = *px
	s.App.Persist()
	writeJSON(w, 200, px)
}

func (s *Server) CreateProxy(w http.ResponseWriter, r *http.Request) { s.saveProxy(w, r, "") }

func (s *Server) UpdateProxy(w http.ResponseWriter, r *http.Request) {
	s.saveProxy(w, r, chi.URLParam(r, "tag"))
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
		writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag))
		return
	}
	for _, p := range kept {
		if p.Via == tag {
			writeError(w, 400, fmt.Sprintf("代理「%s」正把它作为前置出口，请先修改那个代理", p.Name))
			return
		}
	}
	state.Proxies = kept
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// buildProxy converts ProxyIn → Proxy. Link 模式解析分享链接；手动模式直接保存结构化字段，
// 两种模式都落到 OutboundSpec，编辑时可完整回填。
func buildProxy(body *model.ProxyIn) (*model.Proxy, error) {
	px := &model.Proxy{
		Name: strings.TrimSpace(body.Name),
		Link: strings.TrimSpace(body.Link),
		Via:  strings.TrimSpace(body.Via),
	}

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
		px.Spec = n.Spec
		if px.Name == "" {
			px.Name = n.Name
		}
	} else {
		spec := &model.OutboundSpec{
			Protocol: body.Protocol,
			Host:     strings.TrimSpace(body.Host),
			Port:     body.Port,
		}
		switch body.Protocol {
		case "socks", "http":
			if body.Auth != nil {
				spec.User, spec.Pass = strings.TrimSpace(body.Auth.User), body.Auth.Password
			}
		default:
			spec.UUID = strings.TrimSpace(body.UUID)
			spec.Method = strings.TrimSpace(body.Method)
			if body.Protocol != "shadowsocks" {
				spec.Network = strings.TrimSpace(body.Network)
				if spec.Network == "tcp" {
					spec.Network = ""
				}
				spec.Security = strings.TrimSpace(body.TLS)
				if spec.Security == "none" {
					spec.Security = ""
				}
				spec.SNI = strings.TrimSpace(body.SNI)
				spec.Path = strings.TrimSpace(body.Path)
				spec.HostHeader = strings.TrimSpace(body.WsHost)
				spec.Mode = strings.TrimSpace(body.Mode)
				spec.ALPN = strings.TrimSpace(body.ALPN)
				if spec.Security != "" {
					spec.Fingerprint = strings.TrimSpace(body.Fingerprint)
				}
				if body.Protocol == "vless" {
					spec.Flow = strings.TrimSpace(body.Flow)
				}
				if spec.Security == "reality" {
					spec.PublicKey = strings.TrimSpace(body.PublicKey)
					spec.ShortID = strings.TrimSpace(body.ShortId)
					spec.SpiderX = strings.TrimSpace(body.SpiderX)
				}
			}
		}
		if err := service.ValidateSpec(spec); err != nil {
			return nil, err
		}
		px.Spec = spec
	}
	px.Protocol, px.Host, px.Port = px.Spec.Protocol, px.Spec.Host, px.Spec.Port
	return px, nil
}
