package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

var ua = "Mozilla/5.0 (X11; Linux x86_64) Shadowrocket/2.2.49"

// ---------- 订阅列表 ----------

func (s *Server) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.App.State().Subscriptions)
}

func (s *Server) CreateSubscription(w http.ResponseWriter, r *http.Request) {
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

	// 分配 ID
	subID := state.SubSeq
	state.SubSeq++

	sub := model.Subscription{
		ID:        subID,
		URL:       urlStr,
		Remarks:   meta["REMARKS"],
		Status:    meta["STATUS"],
		FetchedAt: time.Now().Unix(),
	}
	state.Subscriptions = append(state.Subscriptions, sub)

	// 合并节点（去重 host:port）
	mergeNodes(state, parsed)
	pruneBalancers(state)

	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 201, map[string]any{
		"subscription": sub,
		"nodes_added":  len(parsed),
		"nodes_total":  len(state.Nodes),
		"skipped":      len(skipped),
	})
}

func (s *Server) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "无效的订阅 ID"); return
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	var kept []model.Subscription
	found := false
	for _, sub := range state.Subscriptions {
		if sub.ID == id {
			found = true
		} else {
			kept = append(kept, sub)
		}
	}
	if !found {
		writeError(w, 404, fmt.Sprintf("订阅 %d 不存在", id)); return
	}
	state.Subscriptions = kept
	s.App.Persist()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) FetchSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "无效的订阅 ID"); return
	}

	s.App.Store().Lock()
	state := s.App.State()
	var target *model.Subscription
	for i := range state.Subscriptions {
		if state.Subscriptions[i].ID == id {
			target = &state.Subscriptions[i]
			break
		}
	}
	if target == nil {
		s.App.Store().Unlock()
		writeError(w, 404, fmt.Sprintf("订阅 %d 不存在", id)); return
	}
	urlStr := target.URL
	s.App.Store().Unlock()

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
	state = s.App.State()
	for i := range state.Subscriptions {
		if state.Subscriptions[i].ID == id {
			state.Subscriptions[i].Remarks = meta["REMARKS"]
			state.Subscriptions[i].Status = meta["STATUS"]
			state.Subscriptions[i].FetchedAt = time.Now().Unix()
			break
		}
	}
	mergeNodes(state, parsed)
	pruneBalancers(state)
	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 200, map[string]any{
		"nodes_added": len(parsed),
		"nodes_total": len(state.Nodes),
		"skipped":     len(skipped),
	})
}

func (s *Server) FetchAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	subs := make([]model.Subscription, len(s.App.State().Subscriptions))
	copy(subs, s.App.State().Subscriptions)
	s.App.Store().Unlock()

	if len(subs) == 0 {
		writeError(w, 400, "没有订阅"); return
	}

	type result struct {
		nodes   []service.NodeRaw
		meta    map[string]string
		skipped int
		err     error
		subID   int
	}
	results := make([]result, len(subs))

	for i, sub := range subs {
		text, err := fetchURL(sub.URL, s.App.Config().SubscriptionAllowInternal)
		if err != nil {
			results[i] = result{subID: sub.ID, err: err}
			continue
		}
		links, meta := service.ExtractLinks(text)
		parsed, skipped := service.ParseLinks(links)
		service.AssignTags(parsed)
		results[i] = result{subID: sub.ID, nodes: parsed, meta: meta, skipped: len(skipped)}
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()

	state := s.App.State()
	var allNodes []service.NodeRaw
	totalSkipped := 0
	for _, res := range results {
		if res.err != nil {
			continue
		}
		for j := range state.Subscriptions {
			if state.Subscriptions[j].ID == res.subID {
				state.Subscriptions[j].Remarks = res.meta["REMARKS"]
				state.Subscriptions[j].Status = res.meta["STATUS"]
				state.Subscriptions[j].FetchedAt = time.Now().Unix()
				break
			}
		}
		allNodes = append(allNodes, res.nodes...)
		totalSkipped += res.skipped
	}
	if len(allNodes) == 0 {
		writeError(w, 400, "所有订阅均未解析到可用节点"); return
	}

	state.Nodes = nil
	mergeNodes(state, allNodes)
	pruneBalancers(state)
	s.App.PruneDangling()
	s.App.Persist()
	writeJSON(w, 200, map[string]any{
		"nodes_total": len(state.Nodes),
		"skipped":     totalSkipped,
	})
}

// ---------- 节点 ----------

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

// ---------- helpers ----------

func mergeNodes(state *model.PanelState, incoming []service.NodeRaw) {
	seen := map[string]bool{}
	for _, n := range state.Nodes {
		seen[fmt.Sprintf("%s:%d", n.Host, n.Port)] = true
	}
	for _, n := range incoming {
		key := fmt.Sprintf("%s:%d", n.Host, n.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		state.Nodes = append(state.Nodes, model.Node{
			Name: n.Name, Type: n.Type, Host: n.Host, Port: n.Port,
			Tag: n.Tag, Outbound: n.Outbound,
		})
	}
	// 重新分配 tag
	for i := range state.Nodes {
		state.Nodes[i].Tag = fmt.Sprintf("node-%d", i)
	}
}

func pruneBalancers(state *model.PanelState) {
	nodeTags := map[string]bool{}
	for _, n := range state.Nodes {
		nodeTags[n.Tag] = true
	}
	var kept []model.Balancer
	for _, b := range state.Balancers {
		valid := true
		for _, t := range b.Nodes {
			if !nodeTags[t] {
				valid = false
				break
			}
		}
		if valid {
			kept = append(kept, b)
		}
	}
	state.Balancers = kept
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
