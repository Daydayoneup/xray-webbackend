package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"xray-panel/internal/model"
)

func streamOfLink(t *testing.T, link string) map[string]any {
	t.Helper()
	nodes, skipped := ParseLinks([]string{link})
	if len(nodes) != 1 {
		t.Fatalf("parse %s: skipped=%+v", link, skipped)
	}
	ss, _ := nodes[0].Outbound["streamSettings"].(map[string]any)
	return ss
}

func TestVLessGRPCServiceName(t *testing.T) {
	ss := streamOfLink(t, "vless://11111111-1111-1111-1111-111111111111@a.com:443?type=grpc&serviceName=mysvc&security=tls&sni=a.com&fp=chrome#g")
	g := ss["grpcSettings"].(map[string]any)
	if g["serviceName"] != "mysvc" {
		t.Errorf("serviceName = %v", g["serviceName"])
	}
	ts := ss["tlsSettings"].(map[string]any)
	if ts["fingerprint"] != "chrome" {
		t.Errorf("tls fingerprint should be kept, got %v", ts)
	}
}

func TestVMessGRPCNoSlashServiceName(t *testing.T) {
	raw := `{"ps":"g","add":"a.com","port":443,"id":"uuid-1","net":"grpc","path":"","tls":"tls"}`
	ss := streamOfLink(t, "vmess://"+b64encodeGo(raw))
	if g := ss["grpcSettings"].(map[string]any); g["serviceName"] != "" {
		t.Errorf("grpc serviceName should be empty, got %v", g["serviceName"])
	}
}

func TestH2AndXHTTPKeepPath(t *testing.T) {
	ss := streamOfLink(t, "vless://11111111-1111-1111-1111-111111111111@a.com:443?type=http&path=%2Fh2&host=a.com&security=tls#h2")
	if ss["network"] != "h2" || ss["httpSettings"].(map[string]any)["path"] != "/h2" {
		t.Errorf("h2 stream = %v", ss)
	}
	ss = streamOfLink(t, "vless://11111111-1111-1111-1111-111111111111@a.com:443?type=xhttp&path=%2Fx&mode=auto&security=tls#x")
	x := ss["xhttpSettings"].(map[string]any)
	if ss["network"] != "xhttp" || x["path"] != "/x" || x["mode"] != "auto" {
		t.Errorf("xhttp stream = %v", ss)
	}
}

func TestWSOmitsEmptyHostHeader(t *testing.T) {
	ss := streamOfLink(t, "vless://11111111-1111-1111-1111-111111111111@a.com:80?type=ws&path=%2Fws#w")
	if _, ok := ss["wsSettings"].(map[string]any)["headers"]; ok {
		t.Errorf("empty Host header should be omitted: %v", ss["wsSettings"])
	}
}

func TestTrojanRespectsSecurityParam(t *testing.T) {
	ss := streamOfLink(t, "trojan://pw@a.com:443?security=reality&pbk=KEY&sid=ab&sni=b.com#r")
	if ss["security"] != "reality" {
		t.Errorf("security = %v", ss["security"])
	}
	ss = streamOfLink(t, "trojan://pw@a.com:443#t")
	if ss["security"] != "tls" {
		t.Errorf("trojan default security = %v", ss["security"])
	}
}

func TestSSVariants(t *testing.T) {
	// SIP002 base64 userinfo + IPv6
	link := "ss://" + b64encodeGo("aes-256-gcm:pw") + "@[2001:db8::1]:8388#v6"
	nodes, skipped := ParseLinks([]string{link})
	if len(nodes) != 1 || nodes[0].Host != "2001:db8::1" || nodes[0].Port != 8388 {
		t.Fatalf("ipv6 ss: nodes=%+v skipped=%+v", nodes, skipped)
	}
	// url-encoded userinfo（2022 系列加密）
	nodes, _ = ParseLinks([]string{"ss://2022-blake3-aes-128-gcm:a%2Bb%3D@1.2.3.4:443#enc"})
	if len(nodes) != 1 || nodes[0].Spec.UUID != "a+b=" || nodes[0].Spec.Method != "2022-blake3-aes-128-gcm" {
		t.Fatalf("urlencoded ss: %+v", nodes)
	}
	// 带插件的节点无法在 Xray 中使用，必须跳过并给出原因
	_, skipped = ParseLinks([]string{"ss://" + b64encodeGo("aes-256-gcm:pw") + "@1.2.3.4:443/?plugin=obfs-local%3Bobfs%3Dhttp#p"})
	if len(skipped) != 1 || !strings.Contains(skipped[0].Detail, "插件") {
		t.Errorf("plugin ss should be skipped with reason, got %+v", skipped)
	}
}

func TestSpecFromOutboundRoundtrip(t *testing.T) {
	specs := []*model.OutboundSpec{
		{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u", Flow: "xtls-rprx-vision", Security: "reality", SNI: "b.com", Fingerprint: "chrome", PublicKey: "k", ShortID: "s"},
		{Protocol: "vmess", Host: "a.com", Port: 443, UUID: "u", Network: "ws", Security: "tls", SNI: "a.com", Path: "/ws", HostHeader: "h.com"},
		{Protocol: "trojan", Host: "a.com", Port: 443, UUID: "pw", Network: "grpc", Security: "tls", Path: "svc"},
		{Protocol: "shadowsocks", Host: "a.com", Port: 8388, UUID: "pw", Method: "aes-128-gcm"},
		{Protocol: "socks", Host: "10.0.0.1", Port: 1080, User: "u", Pass: "p"},
	}
	for _, s := range specs {
		ob := BuildOutbound(s)
		data, _ := json.Marshal(ob)
		var back map[string]any
		json.Unmarshal(data, &back)
		got, ok := SpecFromOutbound(back)
		if !ok {
			t.Fatalf("%s: SpecFromOutbound failed", s.Protocol)
		}
		a, _ := json.Marshal(BuildOutbound(s))
		b, _ := json.Marshal(BuildOutbound(got))
		if string(a) != string(b) {
			t.Errorf("%s roundtrip mismatch:\n%s\n%s", s.Protocol, a, b)
		}
	}
}

func TestDescribeFromAppliedJSON(t *testing.T) {
	// config.json 读回来的是 []any，不是 []string
	var r map[string]any
	json.Unmarshal([]byte(`{"domain":["domain:google.com","domain:youtube.com"],"outboundTag":"x"}`), &r)
	if d := Describe(r); d != "域名后缀 google.com, youtube.com" {
		t.Errorf("describe = %s", d)
	}
	json.Unmarshal([]byte(`{"ip":["geoip:cn"]}`), &r)
	if d := Describe(r); d != "地区IP cn" {
		t.Errorf("describe = %s", d)
	}
}

func TestRuleMultiValueAndValidation(t *testing.T) {
	r := RuleToXray(map[string]any{"type": "geoip", "value": "cn, jp", "outbound": "direct"}, nil)
	if ips := r["ip"].([]string); len(ips) != 2 || ips[1] != "geoip:jp" {
		t.Errorf("ip = %v", r["ip"])
	}
	cases := map[[2]string]bool{
		{"port", "443"}: true, {"port", "1000-2000, 8443"}: true, {"port", "abc"}: false,
		{"ip", "1.1.1.0/24"}: true, {"ip", "1.1.1"}: false,
		{"domain-suffix", "google.com"}: true, {"domain-suffix", "https://google.com"}: false,
		{"geosite", "geolocation-!cn"}: true, {"geosite", ""}: false,
	}
	for c, ok := range cases {
		if err := ValidateRuleValue(c[0], c[1]); (err == nil) != ok {
			t.Errorf("ValidateRuleValue(%s,%q) err=%v want ok=%v", c[0], c[1], err, ok)
		}
	}
}

func TestBuildWarningsAndFallbacks(t *testing.T) {
	spec := &model.OutboundSpec{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u"}
	st := &model.PanelState{
		Nodes: []model.Node{
			{Tag: "node-a", Name: "A", Spec: spec},
			{Tag: "node-gone", Name: "Gone", Spec: spec, Missing: true},
		},
		Proxies: []model.Proxy{
			{Tag: "px-0", Name: "落地", Via: "node-a", Spec: &model.OutboundSpec{Protocol: "socks", Host: "9.9.9.9", Port: 1080}},
			{Tag: "px-1", Name: "坏链", Via: "node-gone", Spec: &model.OutboundSpec{Protocol: "socks", Host: "9.9.9.9", Port: 1080}},
		},
		Rules: []model.Rule{
			{Type: "domain-suffix", Value: "a.com", Outbound: "node-gone", Enabled: true},
			{Type: "domain-suffix", Value: "b.com", Outbound: "px-0", Enabled: true},
		},
		DefaultOutbound: "node-gone",
	}
	res := Build(st, BuildOpts{})
	if len(res.Warnings) < 3 {
		t.Errorf("expected warnings for missing rule/default/broken chain, got %v", res.Warnings)
	}
	cfg := res.Config
	if cfg["routing"].(map[string]any)["domainStrategy"] != "IPIfNonMatch" {
		t.Error("default domainStrategy should be IPIfNonMatch")
	}
	var px0 map[string]any
	for _, ob := range cfg["outbounds"].([]any) {
		m := ob.(map[string]any)
		if m["tag"] == "px-0" {
			px0 = m
		}
		if m["tag"] == "node-gone" || m["tag"] == "px-1" {
			t.Errorf("%v should be skipped", m["tag"])
		}
	}
	if px0 == nil || px0["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["dialerProxy"] != "node-a" {
		t.Errorf("px-0 should chain via node-a: %v", px0)
	}
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if tail := rules[len(rules)-1].(map[string]any); tail["outboundTag"] != "node-a" {
		t.Errorf("default should fall back to node-a, got %v", tail)
	}
}

func TestBuildWithoutNodesDefaultsDirect(t *testing.T) {
	cfg := BuildConfig(&model.PanelState{Inbounds: []model.Inbound{{Tag: "in-0", Protocol: "socks", Port: 1080}}})
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if tail := rules[len(rules)-1].(map[string]any); tail["outboundTag"] != "direct" {
		t.Errorf("no nodes → default direct, got %v", tail)
	}
}

func TestObservatoryOnlyProbesMembers(t *testing.T) {
	spec := &model.OutboundSpec{Protocol: "vless", Host: "a.com", Port: 443, UUID: "u"}
	cfg := BuildConfig(&model.PanelState{
		Nodes:     []model.Node{{Tag: "node-a", Spec: spec}, {Tag: "node-b", Spec: spec}, {Tag: "node-c", Spec: spec}},
		Balancers: []model.Balancer{{Tag: "auto-0", Nodes: []string{"node-a", "node-c"}}},
	})
	sel := cfg["observatory"].(map[string]any)["subjectSelector"].([]string)
	if len(sel) != 2 || sel[0] != "node-a" || sel[1] != "node-c" {
		t.Errorf("subjectSelector = %v", sel)
	}
}

func TestParseUserinfo(t *testing.T) {
	u := ParseUserinfo("upload=100; download=200; total=1000; expire=1700000000")
	if u == nil || u.Upload != 100 || u.Download != 200 || u.Total != 1000 || u.Expire != 1700000000 {
		t.Errorf("usage = %+v", u)
	}
	if ParseUserinfo("") != nil {
		t.Error("empty header → nil")
	}
}

func TestLooksLikeClash(t *testing.T) {
	if !LooksLikeClash("port: 7890\nproxies:\n  - name: a\n") {
		t.Error("clash yaml not detected")
	}
	if LooksLikeClash("vmess://abc") {
		t.Error("link list misdetected as clash")
	}
}

func TestRingLog(t *testing.T) {
	r := newRingLog(3)
	r.Write([]byte("a\nb\nc"))
	r.Write([]byte("d\ne\n"))
	lines := r.Last(10)
	if len(lines) != 3 || !strings.HasSuffix(lines[1], "cd") || !strings.HasSuffix(lines[2], "e") {
		t.Errorf("lines = %v", lines)
	}
}

// 进程崩溃后，状态必须及时反映出来（旧实现会一直显示“运行中”）。
func TestSupervisorDetectsCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "xray")
	os.WriteFile(bin, []byte("#!/bin/sh\necho boom\nexit 3\n"), 0755)
	cfg := filepath.Join(dir, "config.json")
	os.WriteFile(cfg, []byte("{}"), 0600)

	x := NewXrayProc(bin, dir).(*xrayProc)
	x.Start(cfg)
	// 等待崩溃被发现（新建脚本在 macOS 上首次执行可能较慢，不依赖固定时序）
	var st ProcStatus
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if st = x.Status(); strings.Contains(st.LastExit, "3") {
			break
		}
	}
	if !strings.Contains(st.LastExit, "3") {
		t.Fatalf("crash not detected: %+v", st)
	}
	if st.Running || st.State == "running" {
		t.Errorf("status right after crash should not be running: %+v", st)
	}
	found := false
	for _, l := range x.Logs(10) {
		if strings.Contains(l, "boom") {
			found = true
		}
	}
	if !found {
		t.Errorf("logs should capture process output: %v", x.Logs(10))
	}
	x.Stop()
	time.Sleep(50 * time.Millisecond)
	if x.Status().State != "stopped" {
		t.Errorf("after Stop state = %s", x.Status().State)
	}
}

func TestKeywordsAndFilter(t *testing.T) {
	if k := Keywords(" 剩余 | 到期,官网，expire "); len(k) != 4 || k[3] != "expire" {
		t.Errorf("keywords = %v", k)
	}
	kept, dropped := FilterNodes([]NodeRaw{{Name: "香港 01"}, {Name: "官网: x.com"}}, "官网", "")
	if len(kept) != 1 || len(dropped) != 1 {
		t.Errorf("kept=%v dropped=%v", kept, dropped)
	}
	kept, _ = FilterNodes([]NodeRaw{{Name: "香港"}}, "", "")
	if len(kept) != 1 {
		t.Error("empty exclude keeps everything")
	}
}
