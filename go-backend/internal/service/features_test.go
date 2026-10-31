package service

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"xray-panel/internal/model"
)

var testSpec = &model.OutboundSpec{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u"}

func lastRule(cfg map[string]any) map[string]any {
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	return rules[len(rules)-1].(map[string]any)
}

// IPIfNonMatch 下兜底规则必须不能在第一轮（只有域名）命中，否则 geoip 规则对域名流量永不生效。
func TestCatchAllRuleDependsOnDomainStrategy(t *testing.T) {
	st := &model.PanelState{Nodes: []model.Node{{Tag: "node-a", Spec: testSpec}}, DefaultOutbound: "node-a"}
	tail := lastRule(BuildConfig(st))
	if _, ok := tail["network"]; ok || tail["outboundTag"] != "node-a" {
		t.Errorf("IPIfNonMatch tail = %v", tail)
	}
	st.DomainStrategy = "AsIs"
	if tail := lastRule(BuildConfig(st)); tail["network"] != "tcp,udp" {
		t.Errorf("AsIs tail = %v", tail)
	}
}

func TestDefaultOutboundIsFirst(t *testing.T) {
	st := &model.PanelState{Nodes: []model.Node{{Tag: "node-a", Spec: testSpec}, {Tag: "node-b", Spec: testSpec}}, DefaultOutbound: "node-b"}
	first := BuildConfig(st)["outbounds"].([]any)[0].(map[string]any)
	if first["tag"] != "node-b" {
		t.Errorf("first outbound = %v", first["tag"])
	}
}

func TestGroupFilterAndSelect(t *testing.T) {
	st := &model.PanelState{
		Nodes: []model.Node{
			{Tag: "n1", Name: "香港 01", Spec: testSpec, SubID: 1},
			{Tag: "n2", Name: "HK 02", Spec: testSpec, SubID: 2},
			{Tag: "n3", Name: "日本 01", Spec: testSpec, SubID: 1},
			{Tag: "n4", Name: "香港 04", Spec: testSpec, SubID: 1, Missing: true},
		},
		Balancers: []model.Balancer{
			{Tag: "auto-0", Name: "香港", Filter: "香港|hk", Strategy: "leastPing"},
			{Tag: "auto-1", Name: "手动", Nodes: []string{"n1", "n3"}, Strategy: "select", Selected: "n3"},
		},
		Rules:           []model.Rule{{Type: "domain-suffix", Value: "x.com", Outbound: "auto-1", Enabled: true}},
		DefaultOutbound: "auto-0",
	}
	res := Build(st, BuildOpts{})
	if m := res.Members["auto-0"]; len(m) != 2 || m[0] != "n1" || m[1] != "n2" {
		t.Errorf("filter members = %v（不区分大小写，排除失效节点）", m)
	}
	one := 1
	st.Balancers[0].FilterSubID = &one
	if m := Build(st, BuildOpts{}).Members["auto-0"]; len(m) != 1 {
		t.Errorf("filter scoped to sub 1 = %v", m)
	}
	// 手动选择组：规则直接指向所选节点，不生成 Xray balancer
	rules := res.Config["routing"].(map[string]any)["rules"].([]any)
	if r := rules[0].(map[string]any); r["outboundTag"] != "n3" {
		t.Errorf("select group rule = %v", r)
	}
	for _, b := range res.Config["routing"].(map[string]any)["balancers"].([]any) {
		if b.(map[string]any)["tag"] == "auto-1" {
			t.Error("select group must not become an xray balancer")
		}
	}
	st.Nodes[2].Disabled = true // 选中的节点被禁用 → 回退到第一个可用成员并提示
	res = Build(st, BuildOpts{})
	if res.Selected["auto-1"] != "n1" || len(res.Warnings) == 0 {
		t.Errorf("selected fallback = %v warnings=%v", res.Selected, res.Warnings)
	}
}

func TestMetricsAndDNS(t *testing.T) {
	st := &model.PanelState{DNS: model.DNSSettings{Mode: "split"}}
	cfg := Build(st, BuildOpts{MetricsPort: 10085}).Config
	for _, k := range []string{"stats", "metrics", "policy", "dns"} {
		if cfg[k] == nil {
			t.Errorf("missing %s", k)
		}
	}
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if r := rules[0].(map[string]any); r["outboundTag"] != "metrics" {
		t.Errorf("metrics rule must be first: %v", r)
	}
	if r := rules[1].(map[string]any); r["outboundTag"] != "direct" || strList(r["inboundTag"])[0] != "dns-in" {
		t.Errorf("domestic DNS should go direct: %v", r)
	}
	if cfg := BuildConfig(&model.PanelState{}); cfg["dns"] != nil || cfg["stats"] != nil {
		t.Error("system DNS / no metrics → no dns/stats sections")
	}
}

func TestEncryptedInboundsAndShareLinks(t *testing.T) {
	priv, pub := NewRealityKeys()
	if p2, err := RealityPublicKey(priv); err != nil || p2 != pub {
		t.Fatalf("derive pubkey: %v %s %s", err, p2, pub)
	}
	vl := &model.Inbound{Tag: "in-2", Protocol: "vless", Port: 443, Listen: "0.0.0.0",
		Users:   []model.InboundUser{{Name: "alice", Secret: NewUUID()}, {Name: "bob", Secret: NewUUID(), Disabled: true}},
		Reality: &model.RealityConf{Dest: "www.microsoft.com:443", ServerNames: []string{"www.microsoft.com"}, PrivateKey: priv, PublicKey: pub, ShortIDs: []string{"abcd"}}}
	ob := BuildInbound(vl)
	clients := ob["settings"].(map[string]any)["clients"].([]any)
	if len(clients) != 1 || clients[0].(map[string]any)["email"] != "alice" {
		t.Errorf("clients = %v", clients)
	}
	link := ShareLink(vl, vl.Users[0], "1.2.3.4")
	nodes, skipped := ParseLinks([]string{link})
	if len(nodes) != 1 || nodes[0].Spec.PublicKey != pub || nodes[0].Spec.Security != "reality" || nodes[0].Spec.Flow != "xtls-rprx-vision" {
		t.Fatalf("vless share link not parseable: %s %+v", link, skipped)
	}
	ss := &model.Inbound{Tag: "in-3", Protocol: "shadowsocks", Port: 8388, SSMethod: "2022-blake3-aes-128-gcm", SSKey: NewSSKey("2022-blake3-aes-128-gcm"),
		Users: []model.InboundUser{{Name: "carol", Secret: NewSSKey("2022-blake3-aes-128-gcm")}}}
	nodes, _ = ParseLinks([]string{ShareLink(ss, ss.Users[0], "example.com")})
	if len(nodes) != 1 || nodes[0].Spec.UUID != ss.SSKey+":"+ss.Users[0].Secret {
		t.Fatalf("ss2022 share link: %+v", nodes)
	}
	if !ValidSSKey(ss.SSMethod, ss.SSKey) || ValidSSKey("2022-blake3-aes-256-gcm", ss.SSKey) {
		t.Error("ss key length validation")
	}
}

// ---- geo 数据与规则测试 ----

func geoEntry(code string, body []byte) []byte {
	var e []byte
	e = protowire.AppendTag(e, 1, protowire.BytesType)
	e = protowire.AppendString(e, code)
	e = append(e, body...)
	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	return protowire.AppendBytes(out, e)
}

func geoipDat(code, cidr string) []byte {
	_, n, _ := net.ParseCIDR(cidr)
	ones, _ := n.Mask.Size()
	ip := n.IP.To4()
	var c []byte
	c = protowire.AppendTag(c, 1, protowire.BytesType)
	c = protowire.AppendBytes(c, ip)
	c = protowire.AppendTag(c, 2, protowire.VarintType)
	c = protowire.AppendVarint(c, uint64(ones))
	var body []byte
	body = protowire.AppendTag(body, 2, protowire.BytesType)
	body = protowire.AppendBytes(body, c)
	return geoEntry(code, body)
}

func geositeDat(code string, typ int, value string) []byte {
	var d []byte
	d = protowire.AppendTag(d, 1, protowire.VarintType)
	d = protowire.AppendVarint(d, uint64(typ))
	d = protowire.AppendTag(d, 2, protowire.BytesType)
	d = protowire.AppendString(d, value)
	var body []byte
	body = protowire.AppendTag(body, 2, protowire.BytesType)
	body = protowire.AppendBytes(body, d)
	return geoEntry(code, body)
}

func writeGeo(t *testing.T) string {
	dir := t.TempDir()
	ip := append(geoipDat("US", "8.8.8.0/24"), geoipDat("CN", "114.114.0.0/16")...)
	site := append(geositeDat("NETFLIX", 2, "netflix.com"), geositeDat("CN", 2, "baidu.com")...)
	os.WriteFile(filepath.Join(dir, "geoip.dat"), ip, 0600)
	os.WriteFile(filepath.Join(dir, "geosite.dat"), site, 0600)
	if err := ValidateGeoFiles(ip, site); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRouteTester(t *testing.T) {
	geo := NewGeoDB(writeGeo(t))
	st := &model.PanelState{
		Nodes: []model.Node{{Tag: "node-a", Name: "A", Spec: testSpec}},
		Rules: []model.Rule{
			{Type: "geosite", Value: "netflix", Outbound: "node-a", Enabled: true},
			{Type: "domain-suffix", Value: "off.com", Outbound: "block", Enabled: false},
			{Type: "geoip", Value: "cn", Outbound: "direct", Enabled: true},
			{Type: "port", Value: "25", Outbound: "block", Enabled: true},
		},
		DefaultOutbound: "node-a",
	}
	res := Build(st, BuildOpts{MetricsPort: 10085})
	resolve := func(h string) ([]net.IP, error) {
		return map[string][]net.IP{"cn-site.com": {net.ParseIP("114.114.1.1")}, "other.com": {net.ParseIP("1.1.1.1")}}[h], nil
	}
	cases := []struct {
		in       string
		outbound string
		userRule int
		kind     string
	}{
		{"https://www.netflix.com/browse", "node-a", 1, "user"},
		{"cn-site.com", "direct", 3, "user"}, // 域名先不命中，解析后命中 geoip:cn
		{"other.com", "node-a", 0, "default"},
		{"192.168.1.10", "direct", 0, "private"},
		{"mail.example.com:25", "block", 4, "user"},
	}
	for _, c := range cases {
		r, err := TestRoute(res.Config, res.RuleSource, geo, c.in, resolve)
		if err != nil {
			t.Fatal(err)
		}
		if r.Outbound != c.outbound || r.UserRule != c.userRule || r.Kind != c.kind {
			t.Errorf("%s → out=%s rule=%d kind=%s, want %s/%d/%s (steps %v)", c.in, r.Outbound, r.UserRule, r.Kind, c.outbound, c.userRule, c.kind, r.Steps)
		}
	}
	// AsIs：域名请求不会解析，geoip 规则不生效
	st.DomainStrategy = "AsIs"
	res = Build(st, BuildOpts{})
	if r, _ := TestRoute(res.Config, res.RuleSource, geo, "cn-site.com", resolve); r.Outbound != "node-a" {
		t.Errorf("AsIs cn-site.com → %s", r.Outbound)
	}
}

func TestLineDiff(t *testing.T) {
	a := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, "\n")
	b := strings.Join([]string{"a", "b", "c", "d", "E", "f", "g", "h", "i", "j", "k"}, "\n")
	d := LineDiff(a, b, 1)
	var ops string
	for _, l := range d {
		ops += l.Op
	}
	if !strings.Contains(ops, "-+") || !strings.HasSuffix(ops, "+") || !strings.Contains(ops, "@") {
		t.Errorf("diff ops = %q %v", ops, d)
	}
}

func TestParseMetrics(t *testing.T) {
	var raw map[string]json.RawMessage
	json.Unmarshal([]byte(`{"stats":{"inbound":{"in-0":{"uplink":10,"downlink":20}},"outbound":{"node-a":{"uplink":1,"downlink":2}},
	  "user":{"alice@in-2":{"uplink":5,"downlink":6}}},
	  "observatory":{"node-a":{"alive":true,"delay":88,"outbound_tag":"node-a","last_seen_time":1700000000}}}`), &raw)
	m := ParseMetrics(raw)
	if m.Inbound["in-0"].Down != 20 || m.User["alice@in-2"].Up != 5 || !m.Observatory["node-a"].Alive || m.Observatory["node-a"].Delay != 88 {
		t.Errorf("metrics = %+v", m)
	}
}
