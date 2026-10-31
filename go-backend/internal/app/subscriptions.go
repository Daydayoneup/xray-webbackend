package app

import (
	"fmt"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// RefreshResult 是一次订阅拉取的结果，直接返回给前端展示。
type RefreshResult struct {
	SubID         int      `json:"id"`
	Name          string   `json:"name"`
	OK            bool     `json:"ok"`
	Error         string   `json:"error,omitempty"`
	Added         int      `json:"added"`
	Updated       int      `json:"updated"`
	Removed       int      `json:"removed"`
	Missing       int      `json:"missing"` // 已从订阅消失但仍被引用、保留为失效的节点
	Total         int      `json:"total"`   // 该订阅当前节点数
	Skipped       int      `json:"skipped"`
	SkippedDetail string   `json:"skipped_detail,omitempty"`
	Filtered      []string `json:"filtered,omitempty"` // 被排除关键字过滤掉的节点名
}

// SubOptions 是新建订阅的参数。Exclude 为 nil 时使用默认排除关键字。
type SubOptions struct {
	URL             string
	Name            string
	AutoUpdateHours int
	Exclude         *string
	NamePrefix      string
}

func subDisplayName(s *model.Subscription) string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Remarks != "":
		return s.Remarks
	}
	if u, err := neturl.Parse(s.URL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	switch {
	default:
		return fmt.Sprintf("订阅 %d", s.ID)
	}
}

func (a *App) findSub(id int) *model.Subscription {
	for i := range a.state.Subscriptions {
		if a.state.Subscriptions[i].ID == id {
			return &a.state.Subscriptions[i]
		}
	}
	return nil
}

// AddSubscription 先拉取验证，成功后才保存订阅。
func (a *App) AddSubscription(o SubOptions) (*model.Subscription, *RefreshResult, error) {
	url := strings.TrimSpace(o.URL)
	if url == "" {
		return nil, nil, fmt.Errorf("订阅链接为空")
	}
	a.store.Lock()
	for _, s := range a.state.Subscriptions {
		if s.URL == url {
			a.store.Unlock()
			return nil, nil, fmt.Errorf("该订阅已存在（%s）", subDisplayName(&s))
		}
	}
	a.store.Unlock()

	fetched, err := a.fetchSub(url, a.config.SubscriptionAllowInternal)
	if err != nil {
		return nil, nil, fmt.Errorf("拉取失败: %v", err)
	}

	a.store.Lock()
	defer a.store.Unlock()
	st := a.state
	exclude := service.DefaultExclude
	if o.Exclude != nil {
		exclude = strings.TrimSpace(*o.Exclude)
	}
	sub := model.Subscription{ID: st.SubSeq, URL: url, Name: strings.TrimSpace(o.Name), AutoUpdateHours: o.AutoUpdateHours,
		Exclude: exclude, NamePrefix: strings.TrimSpace(o.NamePrefix)}
	st.SubSeq++
	st.Subscriptions = append(st.Subscriptions, sub)
	res := a.applyFetched(sub.ID, fetched)
	a.Persist()
	return a.findSub(sub.ID), res, nil
}

// applyFetched 写入一次成功的拉取结果。调用方持锁。
func (a *App) applyFetched(id int, f *service.FetchedSub) *RefreshResult {
	sub := a.findSub(id)
	now := time.Now().Unix()
	sub.TriedAt, sub.FetchedAt, sub.LastError = now, now, ""
	if v := f.Meta["REMARKS"]; v != "" {
		sub.Remarks = v
	}
	sub.Status = f.Meta["STATUS"]
	if f.Usage != nil {
		sub.Usage = f.Usage
	}
	nodes, dropped := service.FilterNodes(f.Nodes, sub.Exclude, sub.NamePrefix)
	sub.Filtered = len(dropped)
	res := a.replaceSubNodes(id, nodes)
	res.SubID, res.Name, res.OK = id, subDisplayName(sub), true
	res.Filtered = dropped
	res.Skipped = len(f.Skipped)
	res.SkippedDetail = service.SummarizeSkipped(f.Skipped)
	return res
}

func (a *App) recordFetchError(id int, err error) *RefreshResult {
	res := &RefreshResult{SubID: id, Error: err.Error()}
	if sub := a.findSub(id); sub != nil {
		sub.TriedAt = time.Now().Unix()
		sub.LastError = err.Error()
		res.Name = subDisplayName(sub)
		for _, n := range a.state.Nodes {
			if n.SubID == id {
				res.Total++
			}
		}
	}
	return res
}

// RefreshSubscription 拉取单个订阅。失败时保留原有节点，仅记录错误。
func (a *App) RefreshSubscription(id int) (*RefreshResult, error) {
	a.store.Lock()
	sub := a.findSub(id)
	if sub == nil {
		a.store.Unlock()
		return nil, fmt.Errorf("订阅 %d 不存在", id)
	}
	url := sub.URL
	a.store.Unlock()

	fetched, err := a.fetchSub(url, a.config.SubscriptionAllowInternal)

	a.store.Lock()
	defer a.store.Unlock()
	if a.findSub(id) == nil {
		return nil, fmt.Errorf("订阅 %d 已被删除", id)
	}
	if err != nil {
		res := a.recordFetchError(id, err)
		a.Persist()
		return res, fmt.Errorf("拉取失败（已保留原有节点）: %v", err)
	}
	res := a.applyFetched(id, fetched)
	a.Persist()
	return res, nil
}

// RefreshAll 并发拉取所有订阅，逐个合并；单个失败不影响其他订阅，也不会删除它的节点。
func (a *App) RefreshAll(ids []int) []*RefreshResult {
	a.store.Lock()
	type job struct {
		id  int
		url string
	}
	var jobs []job
	for _, s := range a.state.Subscriptions {
		if ids == nil || containsInt(ids, s.ID) {
			jobs = append(jobs, job{s.ID, s.URL})
		}
	}
	a.store.Unlock()

	fetched := make([]*service.FetchedSub, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fetched[i], errs[i] = a.fetchSub(j.url, a.config.SubscriptionAllowInternal)
		}(i, j)
	}
	wg.Wait()

	a.store.Lock()
	defer a.store.Unlock()
	results := []*RefreshResult{}
	allOK := ids == nil
	for i, j := range jobs {
		if a.findSub(j.id) == nil {
			continue
		}
		if errs[i] != nil {
			allOK = false
			results = append(results, a.recordFetchError(j.id, errs[i]))
			continue
		}
		results = append(results, a.applyFetched(j.id, fetched[i]))
	}
	if allOK {
		a.retireLegacyNodes()
	}
	a.Persist()
	return results
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// retireLegacyNodes：所有订阅都成功刷新后，仍未被任何订阅认领的旧节点
// 无人引用则删除，被引用则标记为失效。
func (a *App) retireLegacyNodes() {
	for i := range a.state.Nodes {
		if a.state.Nodes[i].SubID == model.LegacySubID {
			a.state.Nodes[i].Missing = true
		}
	}
}

// DeleteSubscription 删除订阅及其节点；仍被引用的节点保留为失效，避免规则静默串线。
func (a *App) DeleteSubscription(id int) error {
	a.store.Lock()
	defer a.store.Unlock()
	st := a.state
	idx := -1
	for i, s := range st.Subscriptions {
		if s.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("订阅 %d 不存在", id)
	}
	st.Subscriptions = append(st.Subscriptions[:idx], st.Subscriptions[idx+1:]...)
	for i := range st.Nodes {
		if st.Nodes[i].SubID == id {
			st.Nodes[i].Missing = true // 无人引用的会在 Persist 中被回收
		}
	}
	return a.Persist()
}

// replaceSubNodes 用新拉取的节点替换某订阅的节点，并尽量沿用旧 tag：
//  1. 严格指纹完全一致；2. 协议+地址+端口+凭据一致（参数微调、旧数据迁移）；
//  3. 同订阅内同名同协议（机场更换了 UUID/端口）。
//
// 调用方持锁。
func (a *App) replaceSubNodes(subID int, incoming []service.NodeRaw) *RefreshResult {
	st := a.state
	res := &RefreshResult{}
	refs := a.References()
	taken := map[string]bool{}
	for _, n := range st.Nodes {
		taken[n.Tag] = true
	}

	// 新节点去重（严格指纹）
	var fresh []service.NodeRaw
	seen := map[string]bool{}
	for _, r := range incoming {
		fp := service.Fingerprint(r.Spec)
		if seen[fp] {
			continue
		}
		seen[fp] = true
		fresh = append(fresh, r)
	}

	looseKey := func(s *model.OutboundSpec) string {
		if s == nil {
			return ""
		}
		return fmt.Sprintf("%s|%s|%d|%s", s.Protocol, strings.ToLower(s.Host), s.Port, s.UUID)
	}
	var candidates []int
	for i, n := range st.Nodes {
		if n.SubID == subID || n.SubID == model.LegacySubID {
			candidates = append(candidates, i)
		}
	}
	used := map[int]bool{}
	match := make([]int, len(fresh))
	sameFP := make([]bool, len(fresh))
	for i := range match {
		match[i] = -1
	}
	passes := []func(r service.NodeRaw, n model.Node) bool{
		func(r service.NodeRaw, n model.Node) bool {
			return n.Spec != nil && service.Fingerprint(n.Spec) == service.Fingerprint(r.Spec)
		},
		func(r service.NodeRaw, n model.Node) bool {
			return n.Spec != nil && looseKey(n.Spec) == looseKey(r.Spec)
		},
		func(r service.NodeRaw, n model.Node) bool {
			return n.SubID == subID && n.Name == r.Name && n.Type == r.Type
		},
	}
	for p, pass := range passes {
		for i, r := range fresh {
			if match[i] >= 0 {
				continue
			}
			for _, ci := range candidates {
				if !used[ci] && pass(r, st.Nodes[ci]) {
					match[i], used[ci], sameFP[i] = ci, true, p == 0
					break
				}
			}
		}
	}

	var newNodes []model.Node
	for i, r := range fresh {
		n := model.Node{Name: r.Name, Type: r.Type, Host: r.Host, Port: r.Port, SubID: subID, Spec: r.Spec}
		if ci := match[i]; ci >= 0 {
			old := st.Nodes[ci]
			n.Tag, n.Disabled = old.Tag, old.Disabled
			if sameFP[i] {
				n.Latency, n.LatencyMode = old.Latency, old.LatencyMode
			}
			res.Updated++
		} else {
			n.Tag = service.NewNodeTag(subID, service.Fingerprint(r.Spec), taken)
			taken[n.Tag] = true
			res.Added++
		}
		newNodes = append(newNodes, n)
	}
	for _, ci := range candidates {
		n := st.Nodes[ci]
		if used[ci] || n.SubID != subID {
			continue
		}
		if refs[n.Tag] > 0 {
			n.Missing = true
			newNodes = append(newNodes, n)
			res.Missing++
		} else {
			res.Removed++
		}
	}
	res.Total = len(newNodes)

	// 保持列表顺序：该订阅的节点整体放在它原来的位置
	var out []model.Node
	inserted := false
	for i, n := range st.Nodes {
		if n.SubID == subID || used[i] {
			if !inserted {
				out = append(out, newNodes...)
				inserted = true
			}
			continue
		}
		out = append(out, n)
	}
	if !inserted {
		out = append(out, newNodes...)
	}
	st.Nodes = out
	return res
}
