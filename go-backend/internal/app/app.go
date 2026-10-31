package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"xray-panel/internal/auth"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
	"xray-panel/internal/store"
)

type Config struct {
	Store                     store.Store
	XrayProc                  service.XrayProc
	ConfigPath                string
	PanelPort                 int
	Password                  *string
	SubscriptionAllowInternal bool
	DataDir                   string // 历史快照、geo 数据、流量统计的存放目录
	MetricsPort               int
	XrayAssetDir              string
}

type App struct {
	store    store.Store
	xray     service.XrayProc
	sessions *auth.SessionStore
	state    *model.PanelState
	config   Config
	applyMu  sync.Mutex
	// fetchSub 可在测试中替换
	fetchSub func(url string, allowInternal bool) (*service.FetchedSub, error)

	traffic *trafficStore
	online  *onlineTracker
	// onlineBatch 合并短时间内的上下线通知
	onlineBatch *notifyBatch
	monitor     *monitorState
	geoMu       sync.Mutex
	geoDB       *service.GeoDB
	download    func(url string) ([]byte, error) // 可在测试中替换
	fetchList   func(url string) ([]byte, error) // 远程规则列表下载，可在测试中替换
}

func New(cfg Config) (*App, error) {
	state, err := cfg.Store.Load()
	if err != nil {
		return nil, fmt.Errorf("加载状态失败: %w", err)
	}

	a := &App{
		store:    cfg.Store,
		xray:     cfg.XrayProc,
		sessions: auth.NewSessionStore(7 * 86400),
		state:    state,
		config:   cfg,
		fetchSub: service.FetchSubscription,
		monitor:  newMonitorState(),
		online:   newOnlineTracker(),
	}
	// 回调来自 Xray 输出管道，不能在这里等 Store 锁（应用配置时持锁等待 Xray 退出）：只入队，定时器到期后发送
	a.onlineBatch = &notifyBatch{}
	a.online.emit = a.queueOnlineNotify
	a.xray.SetAccessHandler(func(ev service.AccessEvent) { a.online.observe(ev, time.Now()) })
	a.download = a.httpDownload
	a.fetchList = a.downloadRuleList
	if cfg.DataDir != "" {
		a.traffic = loadTrafficStore(cfg.DataDir)
	}
	if upgradeState(a.state) {
		slog.Info("状态数据已升级", "schema_version", a.state.SchemaVersion)
		a.store.Save(a.state)
	}
	a.ensurePassword()
	a.xray.SetAssetDir(a.AssetDir())
	return a, nil
}

// BuildOpts 返回与部署相关的生成参数。
func (a *App) BuildOpts() service.BuildOpts {
	return service.BuildOpts{MetricsPort: a.config.MetricsPort}
}

// Build 生成当前草稿配置。调用方需持有 Store 锁。
func (a *App) Build() service.BuildResult { return service.Build(a.state, a.BuildOpts()) }

// SetFetcher 替换订阅拉取函数（测试用）。
func (a *App) SetFetcher(f func(string, bool) (*service.FetchedSub, error)) { a.fetchSub = f }

func (a *App) ensurePassword() {
	if a.state.Password != nil {
		return
	}
	pw := ""
	src := "随机生成"
	if a.config.Password != nil && *a.config.Password != "" {
		pw = *a.config.Password
		src = "环境变量 PANEL_PASSWORD"
	} else {
		b := make([]byte, 9)
		rand.Read(b)
		pw = hex.EncodeToString(b)
	}
	rec, _ := auth.HashPassword(pw)
	a.state.Password = &model.PasswordRec{Salt: rec.Salt, Hash: rec.Hash}
	a.store.Save(a.state)
	slog.Info(fmt.Sprintf("面板登录密码(%s): %s", src, pw))
}

func (a *App) State() *model.PanelState     { return a.state }
func (a *App) Sessions() *auth.SessionStore { return a.sessions }
func (a *App) Xray() service.XrayProc       { return a.xray }
func (a *App) Store() store.Store           { return a.store }
func (a *App) Config() Config               { return a.config }

// Persist 在保存前统一做引用清理：删除已不存在出口的规则、回收无人引用的失效节点。
// 调用方需持有 Store 锁。
func (a *App) Persist() error {
	a.PruneDangling()
	a.gcMissingNodes()
	return a.store.Save(a.state)
}

func (a *App) BalancerTags() map[string]bool {
	tags := map[string]bool{}
	for _, b := range a.state.Balancers {
		tags[b.Tag] = true
	}
	return tags
}

func (a *App) OutboundTags() map[string]bool {
	tags := map[string]bool{"direct": true, "block": true}
	for _, n := range a.state.Nodes {
		tags[n.Tag] = true
	}
	for _, b := range a.state.Balancers {
		tags[b.Tag] = true
	}
	for _, p := range a.state.Proxies {
		tags[p.Tag] = true
	}
	return tags
}

// References 统计每个出口 tag 被引用的次数（默认出口、规则、节点组成员、链式代理前置）。
func (a *App) References() map[string]int {
	refs := map[string]int{}
	st := a.state
	if st.DefaultOutbound != "" {
		refs[st.DefaultOutbound]++
	}
	for _, r := range st.Rules {
		refs[r.Outbound]++
	}
	for _, b := range st.Balancers {
		for _, m := range b.Nodes {
			refs[m]++
		}
	}
	for _, p := range st.Proxies {
		if p.Via != "" {
			refs[p.Via]++
		}
	}
	for _, ib := range st.Inbounds {
		if ib.DNS != nil && ib.DNS.Via != "" {
			refs[ib.DNS.Via]++
		}
	}
	return refs
}

// PruneDangling 只处理“用户主动删除”导致的悬空引用（自定义代理/节点组被删除）。
// 订阅刷新导致消失的节点不会走到这里：它们被标记为 Missing 并保留，直到不再被引用。
func (a *App) PruneDangling() {
	valid := a.OutboundTags()
	if !valid[a.state.DefaultOutbound] {
		a.state.DefaultOutbound = "direct"
		for _, n := range a.state.Nodes {
			if !n.Missing && !n.Disabled {
				a.state.DefaultOutbound = n.Tag
				break
			}
		}
	}
	sets := map[string]bool{}
	for _, rs := range a.state.RuleSets {
		sets[rs.Tag] = true
	}
	kept := []model.Rule{}
	for _, r := range a.state.Rules {
		if valid[r.Outbound] && (r.Type != "set" || sets[r.Value]) {
			kept = append(kept, r)
		}
	}
	a.state.Rules = kept
	members := map[string]bool{}
	for _, n := range a.state.Nodes {
		members[n.Tag] = true
	}
	for _, p := range a.state.Proxies {
		members[p.Tag] = true
	}
	for i := range a.state.Balancers {
		b := &a.state.Balancers[i]
		var ms []string
		for _, m := range b.Nodes {
			if members[m] {
				ms = append(ms, m)
			}
		}
		b.Nodes = ms
	}
	// 入站 DNS 的出口被删除 → 直连；用户关联的入站被删除 → 解除关联
	inbounds := map[string]bool{}
	for i := range a.state.Inbounds {
		ib := &a.state.Inbounds[i]
		inbounds[ib.Tag] = true
		if ib.DNS != nil && ib.DNS.Via != "" && !valid[ib.DNS.Via] {
			ib.DNS.Via = ""
		}
	}
	for i := range a.state.Users {
		u := &a.state.Users[i]
		var keep []string
		for _, t := range u.Inbounds {
			if inbounds[t] {
				keep = append(keep, t)
			}
		}
		u.Inbounds = keep
	}
}

func (a *App) gcMissingNodes() {
	refs := a.References()
	kept := a.state.Nodes[:0]
	for _, n := range a.state.Nodes {
		if n.Missing && refs[n.Tag] == 0 {
			continue
		}
		kept = append(kept, n)
	}
	a.state.Nodes = kept
}

func (a *App) OutboundLabel(tag string) string {
	switch tag {
	case "direct":
		return "直连"
	case "block":
		return "阻断"
	}
	if in, ok := strings.CutPrefix(tag, "dns-in-"); ok {
		return "入站 DNS（in-" + in + "）"
	}
	for _, n := range a.state.Nodes {
		if n.Tag == tag {
			return n.Name
		}
	}
	for _, b := range a.state.Balancers {
		if b.Tag == tag {
			return "⚖ " + b.Name + "(节点组)"
		}
	}
	for _, p := range a.state.Proxies {
		if p.Tag == tag {
			return "🛰 " + p.Name + "(自定义)"
		}
	}
	return tag
}

// OutboundInfo 是给前端下拉框/列表用的出口信息。
type OutboundInfo struct {
	Tag       string `json:"tag"`
	Label     string `json:"label"`
	Kind      string `json:"kind"` // node | balancer | proxy | builtin
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Refs      int    `json:"refs"`
	SubID     *int   `json:"sub_id,omitempty"`
}

func (a *App) Outbounds() []OutboundInfo {
	res := a.Build()
	refs := a.References()
	out := []OutboundInfo{}
	add := func(tag, label, kind string, subID *int) {
		reason := res.Unavailable[tag]
		out = append(out, OutboundInfo{Tag: tag, Label: label, Kind: kind, Available: reason == "",
			Reason: reason, Refs: refs[tag], SubID: subID})
	}
	for _, b := range a.state.Balancers {
		add(b.Tag, "⚖ "+b.Name+"(节点组)", "balancer", nil)
	}
	for _, p := range a.state.Proxies {
		add(p.Tag, "🛰 "+p.Name+"(自定义)", "proxy", nil)
	}
	for i := range a.state.Nodes {
		n := &a.state.Nodes[i]
		id := n.SubID
		add(n.Tag, n.Name, "node", &id)
	}
	add("direct", "直连", "builtin", nil)
	add("block", "阻断", "builtin", nil)
	return out
}

// CheckVia 校验链式代理的前置出口：必须是节点或其他自定义代理，且不能形成环。
func (a *App) CheckVia(self, via string) error {
	if via == "" {
		return nil
	}
	if via == self {
		return fmt.Errorf("前置出口不能是自己")
	}
	isNode := false
	for _, n := range a.state.Nodes {
		if n.Tag == via {
			isNode = true
		}
	}
	if isNode {
		return nil
	}
	proxies := map[string]string{}
	for _, p := range a.state.Proxies {
		proxies[p.Tag] = p.Via
	}
	if _, ok := proxies[via]; !ok {
		return fmt.Errorf("前置出口 %s 不存在（只能选择节点或其他自定义代理）", via)
	}
	for cur, steps := via, 0; cur != "" && steps < 32; steps++ {
		if cur == self {
			return fmt.Errorf("链式代理形成了循环")
		}
		cur = proxies[cur]
	}
	return nil
}

// AppliedConfig 读取当前生效的 config.json，未应用过返回 nil。
func (a *App) AppliedConfig() map[string]any {
	data, err := os.ReadFile(a.config.ConfigPath)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func configKey(c map[string]any) string {
	data, _ := json.Marshal(map[string]any{
		"i": c["inbounds"], "o": c["outbounds"],
		"r": c["routing"], "ob": c["observatory"], "d": c["dns"], "m": c["metrics"],
	})
	return string(data)
}

// Dirty 判断草稿与已生效配置是否不同；从未应用过时总是 true。
func (a *App) Dirty(applied map[string]any) bool {
	if applied == nil {
		return true
	}
	// 经过一次 JSON 往返，消除 []string 与 []any 等类型差异
	draft := a.Build().Config
	var norm map[string]any
	data, _ := json.Marshal(draft)
	json.Unmarshal(data, &norm)
	return configKey(norm) != configKey(applied)
}
