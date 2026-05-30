package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

var ua = "Mozilla/5.0 (X11; Linux x86_64) Shadowrocket/2.2.49"

func (s *Server) GetSubscription(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Subscription)
}

func (s *Server) SetSubscription(w http.ResponseWriter, r *http.Request) {
	var body model.SubscriptionIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误"); return
	}
	urlStr := strings.TrimSpace(body.URL)
	if urlStr == "" {
		writeError(w, 400, "订阅链接为空"); return
	}

	text, err := fetchURL(urlStr, s.App.Config().SubscriptionAllowInternal)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("拉取失败: %v", err)); return
	}

	links, meta := service.ExtractLinks(text)
	parsed, skipped := service.ParseLinks(links)
	service.AssignTags(parsed)
	if len(parsed) == 0 {
		writeError(w, 400, "未解析到任何 Xray 可用节点"); return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	state.Nodes = make([]model.Node, len(parsed))
	for i, n := range parsed {
		state.Nodes[i] = model.Node{
			Name: n.Name, Type: n.Type, Host: n.Host, Port: n.Port,
			Tag: n.Tag, Outbound: n.Outbound,
		}
	}
	state.Subscription = model.Subscription{
		URL: urlStr, Remarks: meta["REMARKS"], Status: meta["STATUS"],
		FetchedAt: time.Now().Unix(),
	}
	nodeTags := map[string]bool{}
	for _, n := range state.Nodes {
		nodeTags[n.Tag] = true
	}
	var kept []model.Balancer
	for _, b := range state.Balancers {
		valid := true
		for _, t := range b.Nodes {
			if !nodeTags[t] {
				valid = false; break
			}
		}
		if valid {
			kept = append(kept, b)
		}
	}
	state.Balancers = kept
	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 200, map[string]any{
		"nodes": state.Nodes, "skipped": len(skipped),
		"subscription": state.Subscription,
	})
}

func (s *Server) ListNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Nodes)
}

func (s *Server) TestNodes(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	plain := make([]service.NodeRaw, len(s.App.State().Nodes))
	for i, n := range s.App.State().Nodes {
		plain[i] = service.NodeRaw{Host: n.Host, Port: n.Port}
	}
	s.App.Store().Unlock()

	service.MeasureLatency(plain)

	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	for i := range s.App.State().Nodes {
		s.App.State().Nodes[i].Latency = plain[i].Latency
	}
	s.App.Persist()
	writeJSON(w, 200, s.App.State().Nodes)
}

func fetchURL(urlStr string, allowInternal bool) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("链接格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("只支持 http/https 订阅链接")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("订阅链接缺少主机名")
	}
	if !allowInternal {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil {
			return "", fmt.Errorf("无法解析订阅域名")
		}
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
				ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return "", fmt.Errorf("订阅域名解析到内网/保留地址 %s,已拒绝", ip)
			}
		}
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return string(data), err
}
