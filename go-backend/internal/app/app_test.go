package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

type memStore struct{ state *model.PanelState }

func (m *memStore) Load() (*model.PanelState, error) {
	if m.state == nil {
		m.state = &model.PanelState{SchemaVersion: currentSchema}
	}
	return m.state, nil
}
func (m *memStore) Save(s *model.PanelState) error { m.state = s; return nil }
func (m *memStore) Lock()                          {}
func (m *memStore) Unlock()                        {}

func vlessLink(name, host string) string {
	return fmt.Sprintf("vless://11111111-1111-1111-1111-111111111111@%s:443?type=tcp&security=tls#%s", host, name)
}

// fakeSubs 模拟订阅服务器：url → 链接列表；errs 中的 url 返回错误
type fakeSubs struct {
	content map[string][]string
	errs    map[string]error
}

func (f *fakeSubs) fetch(url string, _ bool) (*service.FetchedSub, error) {
	if err := f.errs[url]; err != nil {
		return nil, err
	}
	nodes, skipped := service.ParseLinks(f.content[url])
	return &service.FetchedSub{Nodes: nodes, Skipped: skipped, Meta: map[string]string{}}, nil
}

func newTestApp(t *testing.T, st *model.PanelState) (*App, *fakeSubs, *service.FakeXray) {
	t.Helper()
	pw := "pw"
	fx := &service.FakeXray{}
	dir := t.TempDir()
	a, err := New(Config{Store: &memStore{state: st}, XrayProc: fx,
		ConfigPath: filepath.Join(dir, "config.json"), Password: &pw, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeSubs{content: map[string][]string{}, errs: map[string]error{}}
	a.SetFetcher(fs.fetch)
	return a, fs, fx
}

func tagOf(a *App, name string) string {
	for _, n := range a.State().Nodes {
		if n.Name == name {
			return n.Tag
		}
	}
	return ""
}

// 问题 1：订阅节点顺序变化后，规则必须仍指向原来的节点。
func TestRefreshKeepsTagsWhenOrderChanges(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://s"] = []string{vlessLink("HK", "hk.com"), vlessLink("US", "us.com")}
	if _, _, err := a.AddSubscription(SubOptions{URL: "http://s"}); err != nil {
		t.Fatal(err)
	}
	hk := tagOf(a, "HK")
	a.State().Rules = []model.Rule{{Type: "domain-suffix", Value: "x.com", Outbound: hk, Enabled: true}}
	a.State().DefaultOutbound = tagOf(a, "US")

	fs.content["http://s"] = []string{vlessLink("JP", "jp.com"), vlessLink("US", "us.com"), vlessLink("HK", "hk.com")}
	if r := a.RefreshAll(nil); !r[0].OK {
		t.Fatal(r[0].Error)
	}
	if tagOf(a, "HK") != hk {
		t.Fatalf("HK tag changed: %s → %s", hk, tagOf(a, "HK"))
	}
	if a.State().Rules[0].Outbound != hk {
		t.Error("rule should still point to HK")
	}
	if len(a.State().Nodes) != 3 {
		t.Errorf("nodes = %d", len(a.State().Nodes))
	}
}

// 问题 2：拉取失败时保留原有节点。
func TestFailedRefreshKeepsNodes(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://a"] = []string{vlessLink("A1", "a1.com")}
	fs.content["http://b"] = []string{vlessLink("B1", "b1.com")}
	a.AddSubscription(SubOptions{URL: "http://a"})
	a.AddSubscription(SubOptions{URL: "http://b"})

	fs.errs["http://b"] = errors.New("HTTP 503")
	results := a.RefreshAll(nil)
	if len(a.State().Nodes) != 2 || tagOf(a, "B1") == "" {
		t.Fatalf("nodes of failed sub must be kept: %+v", a.State().Nodes)
	}
	var failed *RefreshResult
	for _, r := range results {
		if !r.OK {
			failed = r
		}
	}
	if failed == nil || failed.Error == "" {
		t.Errorf("failure should be reported per subscription: %+v", results)
	}
	if a.findSub(1).LastError == "" {
		t.Error("subscription should record last error")
	}
}

// 节点从订阅消失：被引用的保留为失效（不静默删除规则），未引用的删除；重新出现时恢复同一 tag。
func TestVanishedNodesAndRevival(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://s"] = []string{vlessLink("HK", "hk.com"), vlessLink("US", "us.com"), vlessLink("SG", "sg.com")}
	a.AddSubscription(SubOptions{URL: "http://s"})
	hk := tagOf(a, "HK")
	a.State().Rules = []model.Rule{{Type: "domain-suffix", Value: "x.com", Outbound: hk, Enabled: true}}
	a.State().DefaultOutbound = tagOf(a, "US")

	fs.content["http://s"] = []string{vlessLink("US", "us.com")}
	res, _ := a.RefreshSubscription(0)
	if res.Missing != 1 || res.Removed != 1 {
		t.Errorf("result = %+v", res)
	}
	if len(a.State().Rules) != 1 {
		t.Fatal("rule must not be silently deleted")
	}
	var hkNode *model.Node
	for i := range a.State().Nodes {
		if a.State().Nodes[i].Tag == hk {
			hkNode = &a.State().Nodes[i]
		}
	}
	if hkNode == nil || !hkNode.Missing {
		t.Fatal("referenced HK should be kept as missing")
	}
	if tagOf(a, "SG") != "" {
		t.Error("unreferenced SG should be removed")
	}

	fs.content["http://s"] = []string{vlessLink("US", "us.com"), vlessLink("HK", "hk.com")}
	a.RefreshSubscription(0)
	if tagOf(a, "HK") != hk {
		t.Error("HK should come back with the same tag")
	}
	for _, n := range a.State().Nodes {
		if n.Missing {
			t.Errorf("%s should no longer be missing", n.Name)
		}
	}
}

// 同一 CDN 域名:端口下 path 不同的节点不能被合并。
func TestDedupeByFingerprintNotHostPort(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://s"] = []string{
		"vless://11111111-1111-1111-1111-111111111111@cdn.com:443?type=ws&path=%2Fa&security=tls#A",
		"vless://11111111-1111-1111-1111-111111111111@cdn.com:443?type=ws&path=%2Fb&security=tls#B",
	}
	a.AddSubscription(SubOptions{URL: "http://s"})
	if len(a.State().Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(a.State().Nodes))
	}
}

func TestDeleteSubscriptionKeepsReferencedNodes(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://s"] = []string{vlessLink("HK", "hk.com"), vlessLink("US", "us.com")}
	a.AddSubscription(SubOptions{URL: "http://s"})
	a.State().DefaultOutbound = tagOf(a, "HK")
	if err := a.DeleteSubscription(0); err != nil {
		t.Fatal(err)
	}
	if len(a.State().Nodes) != 1 || !a.State().Nodes[0].Missing {
		t.Errorf("only the referenced node should remain (missing): %+v", a.State().Nodes)
	}
}

// v1 数据：tag 按序号、只有 Xray JSON；升级后 tag 不变、可还原 Spec，刷新时被认领。
func TestUpgradeLegacyState(t *testing.T) {
	legacyOb := service.BuildVLessOutbound("hk.com", 443, "11111111-1111-1111-1111-111111111111", "",
		service.StreamOpts{Network: "tcp", Security: "tls", SNI: "hk.com"})
	st := &model.PanelState{
		Subscriptions:   []model.Subscription{{ID: 0, URL: "http://a"}, {ID: 1, URL: "http://b"}},
		Nodes:           []model.Node{{Tag: "node-0", Name: "HK", Type: "vless", Host: "hk.com", Port: 443, Outbound: legacyOb}},
		Proxies:         []model.Proxy{{Tag: "px-0", Name: "L", Protocol: "vmess", RawOutbound: service.BuildVMessOutbound("v.com", 443, "uid", service.StreamOpts{Network: "ws", Path: "/p"})}},
		Rules:           []model.Rule{{Type: "domain-suffix", Value: "x.com", Outbound: "node-0", Enabled: true}},
		DefaultOutbound: "node-0",
	}
	a, fs, _ := newTestApp(t, st)
	n := a.State().Nodes[0]
	if n.Spec == nil || n.SubID != model.LegacySubID || n.Tag != "node-0" {
		t.Fatalf("node not upgraded: %+v", n)
	}
	if p := a.State().Proxies[0]; p.Spec == nil || p.Spec.UUID != "uid" || p.Spec.Path != "/p" {
		t.Fatalf("proxy spec not restored: %+v", p)
	}
	fs.content["http://a"] = []string{vlessLink("HK", "hk.com")}
	fs.content["http://b"] = []string{vlessLink("US", "us.com")}
	a.RefreshAll(nil)
	if tagOf(a, "HK") != "node-0" || a.State().Rules[0].Outbound != "node-0" {
		t.Errorf("legacy node should be adopted with its tag: %+v", a.State().Nodes)
	}
}

// 问题 8：没有订阅节点也可以应用（只用自定义代理/直连）。
func TestApplyWithoutNodes(t *testing.T) {
	a, _, fx := newTestApp(t, &model.PanelState{SchemaVersion: currentSchema,
		Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Listen: "127.0.0.1", Port: 1080}}})
	if !a.Dirty(a.AppliedConfig()) {
		t.Error("never-applied state should be dirty")
	}
	if _, err := a.Apply(); err != nil {
		t.Fatal(err)
	}
	if !fx.Alive || a.Dirty(a.AppliedConfig()) {
		t.Error("after apply: xray running and clean")
	}
}

// 问题 13：新配置启动失败时回滚到上一版。
func TestApplyRollsBackWhenXrayFails(t *testing.T) {
	a, _, fx := newTestApp(t, &model.PanelState{SchemaVersion: currentSchema,
		Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Listen: "127.0.0.1", Port: 1080}}})
	if _, err := a.Apply(); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(a.config.ConfigPath)

	a.State().Inbounds[0].Port = 2080
	fx.FailStart = true
	_, err := a.Apply()
	var ae *ApplyError
	if !errors.As(err, &ae) || !ae.RolledBack {
		t.Fatalf("expected rollback error, got %v", err)
	}
	now, _ := os.ReadFile(a.config.ConfigPath)
	if string(now) != string(good) {
		t.Error("config.json should be restored to previous version")
	}
	if !a.Dirty(a.AppliedConfig()) {
		t.Error("draft should still be dirty after rollback")
	}
}

func TestCheckViaRejectsCycle(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{SchemaVersion: currentSchema,
		Proxies: []model.Proxy{{Tag: "px-0", Via: "px-1"}, {Tag: "px-1"}}})
	if err := a.CheckVia("px-1", "px-0"); err == nil {
		t.Error("px-1 → px-0 → px-1 should be rejected")
	}
	if err := a.CheckVia("px-0", "px-1"); err != nil {
		t.Errorf("valid chain rejected: %v", err)
	}
}

// 机场塞的信息节点默认被过滤；名称前缀生效；改过滤条件后 tag 仍稳定。
func TestSubscriptionFilterAndPrefix(t *testing.T) {
	a, fs, _ := newTestApp(t, nil)
	fs.content["http://s"] = []string{
		vlessLink("剩余流量：50 GB", "info1.com"), vlessLink("套餐到期：2027-01-01", "info2.com"),
		vlessLink("HK", "hk.com"), vlessLink("US Expire-Free", "us.com"),
	}
	sub, res, err := a.AddSubscription(SubOptions{URL: "http://s", NamePrefix: "A-"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Exclude != service.DefaultExclude {
		t.Errorf("new subscription should use default exclude, got %q", sub.Exclude)
	}
	// "US Expire-Free" 含 expire（不区分大小写）也会被排除
	if len(res.Filtered) != 3 || sub.Filtered != 3 {
		t.Errorf("filtered = %v", res.Filtered)
	}
	hk := tagOf(a, "A-HK")
	if hk == "" || len(a.State().Nodes) != 1 {
		t.Fatalf("nodes = %+v", a.State().Nodes)
	}

	// 用户清空过滤、改前缀：HK 的 tag 不变
	a.findSub(sub.ID).Exclude = ""
	a.findSub(sub.ID).NamePrefix = "B-"
	a.RefreshSubscription(sub.ID)
	if tagOf(a, "B-HK") != hk {
		t.Errorf("HK tag changed after prefix change: %s → %s", hk, tagOf(a, "B-HK"))
	}
	if len(a.State().Nodes) != 4 {
		t.Errorf("without exclude all 4 nodes should be kept, got %d", len(a.State().Nodes))
	}
}

func TestUpgradeSetsDefaultExclude(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{SchemaVersion: 2,
		Subscriptions: []model.Subscription{{ID: 0, URL: "http://a"}}})
	if a.State().Subscriptions[0].Exclude != service.DefaultExclude || a.State().SchemaVersion != currentSchema {
		t.Errorf("upgrade v2→v3 failed: %+v", a.State())
	}
}
