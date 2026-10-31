package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func baseState() *model.PanelState {
	return &model.PanelState{SchemaVersion: currentSchema,
		Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Listen: "127.0.0.1", Port: 1080}}}
}

func TestHistoryAndRestore(t *testing.T) {
	a, _, _ := newTestApp(t, baseState())
	if _, err := a.Apply(); err != nil {
		t.Fatal(err)
	}
	first := a.ListHistory()
	if len(first) != 1 || !strings.Contains(first[0].Summary, "1 个入站") {
		t.Fatalf("history = %+v", first)
	}
	a.State().Inbounds[0].Port = 2080
	a.Apply()
	if d, _ := a.HistoryDiff(first[0].ID); len(d) == 0 {
		t.Error("diff against current should not be empty")
	}
	if _, err := a.RestoreHistory(first[0].ID); err != nil {
		t.Fatal(err)
	}
	if a.State().Inbounds[0].Port != 1080 || a.State().Password == nil {
		t.Errorf("restore should bring port back and keep password: %+v", a.State().Inbounds)
	}
	for i := 0; i < 15; i++ {
		a.writeHistory(&HistoryEntry{Note: "x", State: baseState()})
	}
	if n := len(a.ListHistory()); n != maxHistory {
		t.Errorf("history kept = %d", n)
	}
}

func TestBackupRoundtrip(t *testing.T) {
	a, _, _ := newTestApp(t, baseState())
	a.State().Rules = []model.Rule{{Type: "geoip", Value: "cn", Outbound: "direct", Enabled: true}}
	data, err := a.Export()
	if err != nil || strings.Contains(string(data), `"hash"`) {
		t.Fatalf("export must not contain password: %v", err)
	}
	b, _, _ := newTestApp(t, baseState())
	pw := b.State().Password
	summary, err := b.Import(data)
	if err != nil || !strings.Contains(summary, "1 条规则") {
		t.Fatalf("import: %v %s", err, summary)
	}
	if b.State().Password != pw {
		t.Error("import must keep current password")
	}
	if _, err := b.Import([]byte(`{"foo":1}`)); err == nil {
		t.Error("non-panel file should be rejected")
	}
	if len(b.ListHistory()) == 0 {
		t.Error("import should save an automatic backup first")
	}
}

func TestTrafficAccumulation(t *testing.T) {
	dir := t.TempDir()
	ts := loadTrafficStore(dir)
	now := time.Now()
	m := func(up, down int64) *service.Metrics {
		return &service.Metrics{Inbound: map[string]service.Traffic{"in-0": {Up: up, Down: down}},
			Outbound: map[string]service.Traffic{}, User: map[string]service.Traffic{}}
	}
	ts.add(m(100, 1000), now)
	ts.add(m(150, 1500), now) // +50/+500
	ts.add(m(10, 20), now)    // Xray 重启，计数清零 → +10/+20
	got := ts.Days[dayKey(now)].Inbound["in-0"]
	if got.Up != 160 || got.Down != 1520 {
		t.Errorf("accumulated = %+v", got)
	}
	ts.save()
	if again := loadTrafficStore(dir); again.Days[dayKey(now)].Inbound["in-0"].Down != 1520 {
		t.Error("traffic should persist")
	}

	a, _, _ := newTestApp(t, baseState())
	a.config.MetricsPort = 10085
	a.traffic = ts
	rep := a.Traffic(7)
	if !rep.Enabled || rep.Total.Down != 1520 || len(rep.Daily) != 7 || rep.Inbound[0].Label != "socks:1080" {
		t.Errorf("report = %+v", rep)
	}
}

func TestGroupStatusUsesObservatory(t *testing.T) {
	spec := &model.OutboundSpec{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u"}
	st := baseState()
	st.Nodes = []model.Node{{Tag: "n1", Name: "香港 1", Spec: spec}, {Tag: "n2", Name: "香港 2", Spec: spec}}
	st.Balancers = []model.Balancer{{Tag: "auto-0", Name: "香港", Filter: "香港", Strategy: "leastPing"}}
	a, _, fx := newTestApp(t, st)
	fx.Alive = true
	a.traffic = loadTrafficStore(t.TempDir())
	a.traffic.add(&service.Metrics{Observatory: map[string]service.ProbeState{
		"n1": {Alive: true, Delay: 300}, "n2": {Alive: true, Delay: 80}}}, time.Now())
	g := a.GroupStatuses()[0]
	if g.Current != "n2" || g.Alive != 2 || len(g.Members) != 2 {
		t.Errorf("group = %+v", g)
	}
}

func TestSelectGroupMemberApplies(t *testing.T) {
	spec := &model.OutboundSpec{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u"}
	st := baseState()
	st.Nodes = []model.Node{{Tag: "n1", Name: "A", Spec: spec}, {Tag: "n2", Name: "B", Spec: spec}}
	st.Balancers = []model.Balancer{{Tag: "auto-0", Name: "手动", Nodes: []string{"n1", "n2"}, Strategy: "select"}}
	st.DefaultOutbound = "auto-0"
	a, _, fx := newTestApp(t, st)
	if _, err := a.SelectGroupMember("auto-0", "n2"); err != nil {
		t.Fatal(err)
	}
	cfg := a.AppliedConfig()
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if tail := rules[len(rules)-1].(map[string]any); tail["outboundTag"] != "n2" || !fx.Alive {
		t.Errorf("tail = %v", tail)
	}
	if _, err := a.SelectGroupMember("auto-0", "nope"); err == nil {
		t.Error("non-member should be rejected")
	}
}

func TestNotifyWebhookAndDedupe(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}))
	defer srv.Close()
	st := baseState()
	st.Notify = model.NotifySettings{WebhookURL: srv.URL, Events: DefaultNotifyEvents()}
	st.Subscriptions = []model.Subscription{{ID: 0, Name: "A", Usage: &model.SubUsage{Total: 100, Upload: 50, Download: 45, Expire: time.Now().Unix() + 3600}}}
	a, _, _ := newTestApp(t, st)
	if err := a.SendTestNotify(); err != nil {
		t.Fatal(err)
	}
	a.checkAlerts()
	a.checkAlerts() // 同一提醒 24 小时内只发一次
	for i := 0; i < 50 && count() < 3; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := count(); n != 3 { // 测试消息 + 到期 + 流量
		t.Fatalf("webhook calls = %d", n)
	}
	a.store.Lock()
	st.Notify.Events = nil
	a.store.Unlock()
	a.monitor.mu.Lock()
	a.monitor.sent = map[string]time.Time{}
	a.monitor.mu.Unlock()
	a.checkAlerts()
	time.Sleep(100 * time.Millisecond)
	if count() != 3 {
		t.Error("disabled events must not be sent")
	}
}

func datEntry(code string) []byte {
	var e []byte
	e = protowire.AppendTag(e, 1, protowire.BytesType)
	e = protowire.AppendString(e, code)
	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	return protowire.AppendBytes(out, e)
}

func TestUpdateGeo(t *testing.T) {
	a, _, fx := newTestApp(t, baseState())
	a.config.XrayAssetDir = "/nonexistent"
	fx.Alive = true
	good := datEntry("CN")
	a.download = func(u string) ([]byte, error) { return good, nil }
	info, err := a.UpdateGeo()
	if err != nil || !info.Custom || a.AssetDir() != filepath.Join(a.config.DataDir, "geo") {
		t.Fatalf("update geo: %v %+v", err, info)
	}
	if fx.Restarts == 0 {
		t.Error("xray should be restarted to load new geo data")
	}
	a.download = func(u string) ([]byte, error) { return []byte("<html>not found</html>"), nil }
	if _, err := a.UpdateGeo(); err == nil || a.State().Geo.LastError == "" {
		t.Error("invalid download must be rejected and recorded")
	}
	if b, _ := os.ReadFile(filepath.Join(a.config.DataDir, "geo", "geoip.dat")); string(b) != string(good) {
		t.Error("existing geo data must be kept when an update fails")
	}
	_ = io.EOF
}

func TestUpgradeV4MovesInboundAuth(t *testing.T) {
	a, _, _ := newTestApp(t, &model.PanelState{SchemaVersion: 3,
		Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Port: 1080, Auth: &model.Auth{User: "u", Password: "p"}}}})
	ib := a.State().Inbounds[0]
	us := service.InboundUsers(a.State(), &ib)
	if ib.Auth != nil || len(ib.Users) != 0 || len(us) != 1 || us[0].Name != "u" || us[0].Secret != "p" || len(a.State().Notify.Events) == 0 {
		t.Errorf("upgrade: %+v %+v %+v", ib, us, a.State().Notify)
	}
}
