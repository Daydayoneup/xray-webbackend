package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func TestOnlineTracker(t *testing.T) {
	tr := newOnlineTracker()
	var mu sync.Mutex
	var evs []OnlineEvent
	tr.emit = func(e []OnlineEvent) { mu.Lock(); evs = append(evs, e...); mu.Unlock() }
	t0 := time.Unix(1_700_000_000, 0)
	acc := func(ip, email string) service.AccessEvent {
		return service.AccessEvent{IP: ip, Network: "tcp", Dest: "a.com:443", Inbound: "in-2", Outbound: "direct", Email: email}
	}
	tr.observe(acc("1.1.1.1", "alice@in-2"), t0)
	tr.observe(acc("1.1.1.1", "alice@in-2"), t0.Add(time.Second))
	tr.observe(acc("2.2.2.2", "alice@in-2"), t0.Add(2*time.Second))
	tr.observe(service.AccessEvent{IP: "127.0.0.1", Inbound: service.MetricsInboundTag}, t0)
	kinds := func() string {
		mu.Lock()
		defer mu.Unlock()
		var k []string
		for _, e := range evs {
			k = append(k, e.Kind+":"+e.IP)
		}
		return strings.Join(k, ",")
	}
	if got := kinds(); got != "online:1.1.1.1,new_ip:2.2.2.2" {
		t.Fatalf("events = %s", got)
	}

	// 长连接：没有新访问日志，但有流量 → 保持在线，并计入本次流量
	tr.addTraffic(trafficDelta{User: map[string]service.Traffic{"alice@in-2": {Up: 10, Down: 100}}}, t0.Add(150*time.Second))
	tr.sweep(t0.Add(4 * time.Minute))
	if len(tr.users) != 1 {
		t.Fatal("user with recent traffic should stay online")
	}
	if u := tr.users["in-2|alice@in-2"]; len(u.Addrs) != 1 || u.Addrs["2.2.2.2"] == nil || u.Down != 100 {
		t.Fatalf("idle address should expire, traffic attributed to latest: %+v", u)
	}
	tr.sweep(t0.Add(10 * time.Minute))
	if len(tr.users) != 0 || !strings.HasSuffix(kinds(), "offline:2.2.2.2") {
		t.Fatalf("user should go offline: %s", kinds())
	}
	if last := evs[len(evs)-1]; last.Down != 100 || last.Duration != 150 {
		t.Fatalf("offline event = %+v", last)
	}

	// 未鉴权入站：按 IP 区分；只有一个来源时入站流量归属它
	tr.observe(service.AccessEvent{IP: "3.3.3.3", Inbound: "in-0", Network: "udp"}, t0)
	tr.addTraffic(trafficDelta{Inbound: map[string]service.Traffic{"in-0": {Up: 1, Down: 2}}}, t0)
	if u := tr.users["in-0|ip:3.3.3.3"]; u == nil || u.Down != 2 {
		t.Fatalf("anonymous user = %+v", u)
	}
}

func TestOnlineReportAndNotify(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Title, Body string }
		json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		bodies = append(bodies, m.Title+"\n"+m.Body)
		mu.Unlock()
	}))
	defer srv.Close()
	old := onlineNotifyWindow
	onlineNotifyWindow = 20 * time.Millisecond
	defer func() { onlineNotifyWindow = old }()
	a, _, fx := newTestApp(t, &model.PanelState{SchemaVersion: currentSchema,
		Inbounds: []model.Inbound{{Tag: "in-2", Protocol: "vless", Remark: "外网", Port: 443}},
		Notify:   model.NotifySettings{WebhookURL: srv.URL, Events: []string{"user_online", "user_offline"}}})
	fx.Access(service.AccessEvent{IP: "9.9.9.9", Network: "tcp", Dest: "a.com:443", Inbound: "in-2", Outbound: "direct", Email: "bob@in-2"})
	rep := a.Online(OnlineQuery{})
	if len(rep.Users) != 1 {
		t.Fatalf("users = %+v", rep.Users)
	}
	u := rep.Users[0]
	if u.User != "bob" || u.Protocol != "VLESS + Reality" || u.InboundLabel != "外网 :443" || u.Addrs[0].IP != "9.9.9.9" || u.Addrs[0].Outbound != "直连" {
		t.Fatalf("user = %+v", u)
	}
	if len(rep.Events) != 1 || rep.Events[0].User != "bob" {
		t.Fatalf("events = %+v", rep.Events)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "用户上线") || !strings.Contains(bodies[0], "9.9.9.9") || !strings.Contains(bodies[0], "bob") {
		t.Fatalf("notify = %q", bodies)
	}
}

func TestOnlineQueryAndBatchNotify(t *testing.T) {
	old := onlineNotifyWindow
	onlineNotifyWindow = 50 * time.Millisecond
	defer func() { onlineNotifyWindow = old }()
	var mu sync.Mutex
	var msgs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Title, Body string }
		json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		msgs = append(msgs, m.Title+"\n"+m.Body)
		mu.Unlock()
	}))
	defer srv.Close()
	a, _, fx := newTestApp(t, &model.PanelState{SchemaVersion: currentSchema,
		Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Port: 1080}, {Tag: "in-2", Protocol: "vless", Remark: "外网", Port: 443}},
		Notify:   model.NotifySettings{WebhookURL: srv.URL, Events: []string{"user_online", "user_offline"}}})
	for i := 0; i < 40; i++ {
		fx.Access(service.AccessEvent{IP: fmt.Sprintf("9.9.9.%d", i), Network: "tcp", Inbound: "in-2", Email: fmt.Sprintf("u%02d", i)})
	}
	fx.Access(service.AccessEvent{IP: "192.168.1.2", Network: "tcp", Inbound: "in-0"})
	a.online.addTraffic(trafficDelta{User: map[string]service.Traffic{"u07": {Down: 999}}}, time.Now())

	rep := a.Online(OnlineQuery{Sort: "traffic", Limit: 10})
	if rep.Summary.Users != 41 || rep.Summary.Addrs != 41 || rep.Summary.Anonymous != 1 || rep.Total != 41 || len(rep.Users) != 10 {
		t.Fatalf("summary=%+v total=%d page=%d", rep.Summary, rep.Total, len(rep.Users))
	}
	if rep.Users[0].User != "u07" || rep.Summary.ByInbound[0].Inbound != "in-2" || rep.Summary.ByInbound[0].Users != 40 {
		t.Errorf("sort/by-inbound: %s %+v", rep.Users[0].User, rep.Summary.ByInbound)
	}
	if r := a.Online(OnlineQuery{Q: "9.9.9.1", Limit: 5, Offset: 5}); r.Total != 11 || len(r.Users) != 5 {
		t.Errorf("filter by ip total=%d page=%d", r.Total, len(r.Users))
	}
	if r := a.Online(OnlineQuery{Inbound: "in-0"}); r.Total != 1 || r.Users[0].User != "" {
		t.Errorf("filter by inbound = %+v", r.Users)
	}
	if r := a.Online(OnlineQuery{EventLimit: 5}); len(r.Events) != 5 || r.EventsTotal != 41 {
		t.Errorf("events = %d/%d", len(r.Events), r.EventsTotal)
	}
	if r := a.Online(OnlineQuery{EventLimit: 20, EventOffset: 40}); len(r.Events) != 1 || r.EventsTotal != 41 {
		t.Errorf("last event page = %d/%d", len(r.Events), r.EventsTotal)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(msgs)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "41 位用户上线") || !strings.Contains(msgs[0], "另有 11 条") {
		t.Fatalf("41 events should be merged into one message, got %d: %q", len(msgs), msgs)
	}
}
