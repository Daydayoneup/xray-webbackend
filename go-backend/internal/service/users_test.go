package service

import (
	"encoding/base64"
	"strings"
	"testing"

	"xray-panel/internal/model"
)

func TestSSUserKey(t *testing.T) {
	k32 := NewSSKey("2022-blake3-aes-256-gcm")
	if got := SSUserKey(k32, "2022-blake3-aes-256-gcm"); got != k32 {
		t.Error("32-byte key must be used as-is for 256 methods")
	}
	k16 := SSUserKey(k32, "2022-blake3-aes-128-gcm")
	raw, _ := base64.StdEncoding.DecodeString(k16)
	full, _ := base64.StdEncoding.DecodeString(k32)
	if len(raw) != 16 || string(raw) != string(full[:16]) {
		t.Error("128 methods should use the first 16 bytes")
	}
	old16 := NewSSKey("2022-blake3-aes-128-gcm")
	if SSUserKey(old16, "2022-blake3-aes-128-gcm") != old16 {
		t.Error("migrated 16-byte keys must stay unchanged")
	}
	if d := SSUserKey(old16, "2022-blake3-aes-256-gcm"); !ValidSSKey("2022-blake3-aes-256-gcm", d) || d != SSUserKey(old16, "2022-blake3-aes-256-gcm") {
		t.Error("short key should be extended deterministically")
	}
}

func TestNormalizeDNSServer(t *testing.T) {
	for in, want := range map[string]string{"8.8.8.8": "8.8.8.8:53", " 1.1.1.1:5353 ": "1.1.1.1:5353",
		"2001:4860::8888": "[2001:4860::8888]:53", "[2001:4860::8888]:53": "[2001:4860::8888]:53"} {
		if got, err := NormalizeDNSServer(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"dns.google", "https://1.1.1.1/dns-query", "1.1.1.1:0", ""} {
		if _, err := NormalizeDNSServer(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestBuildUsersAndInboundDNS(t *testing.T) {
	st := &model.PanelState{
		Nodes: []model.Node{{Tag: "node-a", Name: "A", Spec: &model.OutboundSpec{Protocol: "trojan", Host: "1.1.1.1", Port: 443, UUID: "pw"}}},
		Inbounds: []model.Inbound{
			{Tag: "in-0", Protocol: "socks", Port: 1080, DNS: &model.InboundDNS{Server: "8.8.8.8:53", Via: "node-a"}},
			{Tag: "in-1", Protocol: "shadowsocks", Port: 8388, SSMethod: "2022-blake3-aes-128-gcm", SSKey: NewSSKey("2022-blake3-aes-128-gcm"),
				DNS: &model.InboundDNS{Server: "223.5.5.5:53", Via: "gone"}},
			{Tag: "in-2", Protocol: "vless", Port: 443, Reality: &model.RealityConf{}},
		},
		Users: []model.User{
			{Name: "alice", Password: "p1", UUID: NewUUID(), SSKey: NewSSKey("2022-blake3-aes-256-gcm"), Inbounds: []string{"in-0", "in-1"}},
			{Name: "bob", Disabled: true, Password: "p2", Inbounds: []string{"in-0", "in-2"}},
		},
		DefaultOutbound: "direct",
	}
	res := Build(st, BuildOpts{})
	ins := map[string]map[string]any{}
	for _, x := range res.Config["inbounds"].([]any) {
		ins[x.(map[string]any)["tag"].(string)] = x.(map[string]any)
	}
	accounts := ins["in-0"]["settings"].(map[string]any)["accounts"].([]any)
	if len(accounts) != 1 || accounts[0].(map[string]any)["user"] != "alice" {
		t.Errorf("disabled users must be excluded: %v", accounts)
	}
	clients := ins["in-1"]["settings"].(map[string]any)["clients"].([]any)
	if c := clients[0].(map[string]any); c["email"] != "alice" || !ValidSSKey("2022-blake3-aes-128-gcm", c["password"].(string)) {
		t.Errorf("ss client = %v", c)
	}
	if _, ok := ins["in-2"]; ok {
		t.Error("vless inbound without enabled users should be skipped")
	}
	obs := map[string]map[string]any{}
	for _, x := range res.Config["outbounds"].([]any) {
		obs[x.(map[string]any)["tag"].(string)] = x.(map[string]any)
	}
	d0 := obs["dns-in-0"]
	if d0 == nil || d0["settings"].(map[string]any)["redirect"] != "8.8.8.8:53" ||
		d0["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["dialerProxy"] != "node-a" {
		t.Errorf("dns-in-0 = %v", d0)
	}
	if d1 := obs["dns-in-1"]; d1 == nil || d1["streamSettings"] != nil {
		t.Errorf("unavailable via should fall back to direct: %v", d1)
	}
	if !strings.Contains(strings.Join(res.Warnings, "\n"), "DNS 出口") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	found := false
	for _, r := range res.Config["routing"].(map[string]any)["rules"].([]any) {
		m := r.(map[string]any)
		if m["outboundTag"] == "dns-in-0" && m["port"] == "53" {
			found = true
		}
	}
	if !found {
		t.Error("missing inbound DNS routing rule")
	}
}
