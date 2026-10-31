package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/app"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// ---------- 订阅 ----------

type subView struct {
	model.Subscription
	NodeCount    int `json:"node_count"`
	MissingCount int `json:"missing_count"`
}

func (s *Server) subViews() []subView {
	st := s.App.State()
	out := []subView{}
	for _, sub := range st.Subscriptions {
		v := subView{Subscription: sub}
		for _, n := range st.Nodes {
			if n.SubID == sub.ID {
				if n.Missing {
					v.MissingCount++
				} else {
					v.NodeCount++
				}
			}
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) ListSubscriptions(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.subViews())
}

func (s *Server) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	var body model.SubscriptionIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	sub, res, err := s.App.AddSubscription(app.SubOptions{URL: body.URL, Name: body.Name,
		AutoUpdateHours: body.AutoUpdateHours, Exclude: body.Exclude, NamePrefix: body.NamePrefix})
	if err != nil {
		code := 400
		if strings.HasPrefix(err.Error(), "拉取失败") {
			code = 502
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"subscription": sub, "result": res})
}

func (s *Server) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "无效的订阅 ID")
		return
	}
	var body model.SubscriptionUpdateIn
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
	st := s.App.State()
	var sub *model.Subscription
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == id {
			sub = &st.Subscriptions[i]
		}
	}
	if sub == nil {
		writeError(w, 404, fmt.Sprintf("订阅 %d 不存在", id))
		return
	}
	if body.URL != nil {
		u := strings.TrimSpace(*body.URL)
		if u == "" {
			writeError(w, 400, "订阅链接不能为空")
			return
		}
		for _, o := range st.Subscriptions {
			if o.ID != id && o.URL == u {
				writeError(w, 400, "该订阅链接已存在")
				return
			}
		}
		sub.URL = u
	}
	if body.Name != nil {
		sub.Name = strings.TrimSpace(*body.Name)
	}
	if body.AutoUpdateHours != nil {
		sub.AutoUpdateHours = *body.AutoUpdateHours
	}
	if body.Exclude != nil {
		sub.Exclude = strings.TrimSpace(*body.Exclude)
	}
	if body.NamePrefix != nil {
		sub.NamePrefix = strings.TrimSpace(*body.NamePrefix)
	}
	s.App.Persist()
	writeJSON(w, 200, sub)
}

func (s *Server) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "无效的订阅 ID")
		return
	}
	if err := s.App.DeleteSubscription(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) FetchSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "无效的订阅 ID")
		return
	}
	res, err := s.App.RefreshSubscription(id)
	if err != nil {
		if res == nil {
			writeError(w, 404, err.Error())
			return
		}
		writeJSON(w, 502, map[string]any{"detail": err.Error(), "result": res})
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) FetchAllSubscriptions(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	n := len(s.App.State().Subscriptions)
	s.App.Store().Unlock()
	if n == 0 {
		writeError(w, 400, "还没有订阅")
		return
	}
	results := s.App.RefreshAll(nil)
	s.App.Store().Lock()
	total := len(s.App.State().Nodes)
	s.App.Store().Unlock()
	writeJSON(w, 200, map[string]any{"results": results, "nodes_total": total})
}

// ---------- 节点 ----------

type nodeView struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	SubID       int    `json:"sub_id"`
	Network     string `json:"network,omitempty"`
	Security    string `json:"security,omitempty"`
	Disabled    bool   `json:"disabled"`
	Missing     bool   `json:"missing"`
	Invalid     bool   `json:"invalid"`
	Latency     *int   `json:"latency"`
	LatencyMode string `json:"latency_mode,omitempty"`
	Refs        int    `json:"refs"`
}

func (s *Server) nodeViews() []nodeView {
	refs := s.App.References()
	out := []nodeView{}
	for i := range s.App.State().Nodes {
		n := &s.App.State().Nodes[i]
		v := nodeView{Tag: n.Tag, Name: n.Name, Type: n.Type, Host: n.Host, Port: n.Port, SubID: n.SubID,
			Disabled: n.Disabled, Missing: n.Missing, Latency: n.Latency, LatencyMode: n.LatencyMode,
			Refs: refs[n.Tag], Invalid: service.NodeOutbound(n) == nil}
		if n.Spec != nil {
			v.Network, v.Security = selOr(n.Spec.Network, "tcp"), n.Spec.Security
		}
		out = append(out, v)
	}
	return out
}

func selOr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (s *Server) ListNodes(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, s.nodeViews())
}

func (s *Server) UpdateNode(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	var body model.NodeUpdateIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	for i := range s.App.State().Nodes {
		n := &s.App.State().Nodes[i]
		if n.Tag == tag {
			if body.Disabled != nil {
				n.Disabled = *body.Disabled
			}
			s.App.Persist()
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	writeError(w, 404, fmt.Sprintf("节点 %s 不存在", tag))
}

func (s *Server) BatchNodes(w http.ResponseWriter, r *http.Request) {
	var body model.NodeBatchIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	want := map[string]bool{}
	for _, t := range body.Tags {
		want[t] = true
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	changed := 0
	for i := range s.App.State().Nodes {
		if n := &s.App.State().Nodes[i]; want[n.Tag] {
			n.Disabled = body.Disabled
			changed++
		}
	}
	s.App.Persist()
	writeJSON(w, 200, map[string]int{"changed": changed})
}

func (s *Server) TestNodes(w http.ResponseWriter, r *http.Request) {
	var body model.TestNodesIn
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, 400, "请求格式错误")
			return
		}
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	mode := selOr(body.Mode, "real")
	want := map[string]bool{}
	for _, t := range body.Tags {
		want[t] = true
	}

	s.App.Store().Lock()
	var tags []string
	var obs []map[string]any
	var plain []service.NodeRaw
	for i := range s.App.State().Nodes {
		n := &s.App.State().Nodes[i]
		if len(want) > 0 && !want[n.Tag] {
			continue
		}
		tags = append(tags, n.Tag)
		obs = append(obs, service.NodeOutbound(n))
		plain = append(plain, service.NodeRaw{Host: n.Host, Port: n.Port})
	}
	s.App.Store().Unlock()

	results := make([]*int, len(tags))
	fallback := ""
	if mode == "real" {
		res, err := s.App.Xray().Probe(obs, 5*time.Second)
		if err != nil {
			fallback = "真实测速不可用，已改用 TCP 测速：" + err.Error()
			mode = "tcp"
		} else {
			results = res
		}
	}
	if mode == "tcp" {
		service.MeasureLatency(plain)
		for i := range plain {
			results[i] = plain[i].Latency
		}
	}

	byTag := map[string]*int{}
	for i, t := range tags {
		byTag[t] = results[i]
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	for i := range s.App.State().Nodes {
		n := &s.App.State().Nodes[i]
		if v, ok := byTag[n.Tag]; ok {
			n.Latency, n.LatencyMode = v, mode
		}
	}
	s.App.Store().Save(s.App.State())
	writeJSON(w, 200, map[string]any{"nodes": s.nodeViews(), "mode": mode, "fallback": fallback})
}
