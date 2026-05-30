package service

import (
	"testing"

	"xray-panel/internal/model"
)

func TestInboundToXraySocksAuth(t *testing.T) {
	ib := map[string]any{
		"tag": "in-0", "protocol": "socks", "listen": "0.0.0.0", "port": 10808,
		"auth": map[string]any{"user": "u", "pass": "p"},
	}
	result := InboundToXray(ib)
	if result["tag"] != "in-0" {
		t.Errorf("tag = %v", result["tag"])
	}
	settings := result["settings"].(map[string]any)
	if settings["auth"] != "password" {
		t.Errorf("socks with auth should have auth=password")
	}
}

func TestInboundToXrayNoAuth(t *testing.T) {
	ib := map[string]any{"tag": "in-1", "protocol": "socks", "listen": "127.0.0.1", "port": 1080}
	result := InboundToXray(ib)
	settings := result["settings"].(map[string]any)
	if settings["auth"] != "noauth" {
		t.Errorf("socks without auth should have auth=noauth")
	}
}

func TestProxyToXray(t *testing.T) {
	p := map[string]any{
		"tag": "px-0", "protocol": "socks", "host": "10.0.0.1", "port": 1080,
		"auth": map[string]any{"user": "u", "pass": "p"},
	}
	result := ProxyToXray(p)
	settings := result["settings"].(map[string]any)
	servers := settings["servers"].([]any)
	server := servers[0].(map[string]any)
	if server["address"] != "10.0.0.1" {
		t.Errorf("address = %v", server["address"])
	}
}

func TestRuleToXrayDomain(t *testing.T) {
	rule := map[string]any{"type": "domain-suffix", "value": "google.com", "outbound": "px-0"}
	result := RuleToXray(rule, map[string]bool{})
	domains := result["domain"].([]string)
	if domains[0] != "domain:google.com" {
		t.Errorf("domain = %s", domains[0])
	}
}

func TestRuleToXrayBalancer(t *testing.T) {
	rule := map[string]any{"type": "full", "value": "example.com", "outbound": "auto-0"}
	result := RuleToXray(rule, map[string]bool{"auto-0": true})
	if _, ok := result["balancerTag"]; !ok {
		t.Error("balancer should use balancerTag")
	}
}

func TestTemplatesNotEmpty(t *testing.T) {
	if len(Templates) == 0 {
		t.Error("templates should not be empty")
	}
}

func TestDescribe(t *testing.T) {
	r := map[string]any{"domain": []string{"domain:google.com"}}
	if d := Describe(r); d != "域名后缀 google.com" {
		t.Errorf("describe = %s, want 域名后缀 google.com", d)
	}
}

func TestBuildConfigMinimal(t *testing.T) {
	state := &model.PanelState{
		Nodes: []model.Node{
			{Tag: "node-0", Name: "Test", Type: "vmess", Host: "1.2.3.4", Port: 443,
				Outbound: map[string]any{"protocol": "vmess"}},
		},
		Inbounds: []model.Inbound{
			{Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808, UDP: true},
		},
		DefaultOutbound: "node-0",
	}
	cfg := BuildConfig(state)
	outbounds := cfg["outbounds"].([]any)
	if len(outbounds) < 3 {
		t.Fatalf("expected >= 3 outbounds, got %d", len(outbounds))
	}
}

func TestBuildConfigWithBalancers(t *testing.T) {
	state := &model.PanelState{
		Nodes: []model.Node{
			{Tag: "node-0", Name: "N1", Type: "vmess", Host: "1.1.1.1", Port: 443,
				Outbound: map[string]any{"protocol": "vmess"}},
			{Tag: "node-1", Name: "N2", Type: "vless", Host: "2.2.2.2", Port: 443,
				Outbound: map[string]any{"protocol": "vless"}},
		},
		Balancers: []model.Balancer{
			{Tag: "auto-0", Name: "Auto", Nodes: []string{"node-0", "node-1"}, Strategy: "leastPing"},
		},
		Inbounds: []model.Inbound{
			{Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808},
		},
		DefaultOutbound: "auto-0",
	}
	cfg := BuildConfig(state)
	routing := cfg["routing"].(map[string]any)
	if _, ok := routing["balancers"]; !ok {
		t.Error("balancers should exist in routing")
	}
	if _, ok := cfg["observatory"]; !ok {
		t.Error("observatory should exist when balancers are configured")
	}
}

func TestBuildVMessOutbound(t *testing.T) {
	ob := BuildVMessOutbound("1.2.3.4", 443, "test-uuid", StreamOpts{
		Network: "ws", Security: "tls", SNI: "example.com", Path: "/ws",
	})
	if ob["protocol"] != "vmess" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	settings := ob["settings"].(map[string]any)
	vnext := settings["vnext"].([]any)
	first := vnext[0].(map[string]any)
	if first["address"] != "1.2.3.4" {
		t.Errorf("address = %v", first["address"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["security"] != "tls" {
		t.Errorf("security = %v", stream["security"])
	}
}

func TestBuildVLessOutbound(t *testing.T) {
	ob := BuildVLessOutbound("2.2.2.2", 443, "uuid", "xtls-rprx-vision", StreamOpts{
		Network: "tcp", Security: "reality", SNI: "yahoo.com",
		Fingerprint: "chrome", PublicKey: "pubkey", ShortId: "abc",
	})
	if ob["protocol"] != "vless" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["security"] != "reality" {
		t.Errorf("security = %v", stream["security"])
	}
	rs := stream["realitySettings"].(map[string]any)
	if rs["publicKey"] != "pubkey" {
		t.Errorf("publicKey = %v", rs["publicKey"])
	}
}

func TestBuildTrojanOutbound(t *testing.T) {
	ob := BuildTrojanOutbound("3.3.3.3", 443, "password", StreamOpts{
		Network: "grpc", Security: "tls", SNI: "example.com", Path: "myservice",
	})
	if ob["protocol"] != "trojan" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["network"] != "grpc" {
		t.Errorf("network = %v", stream["network"])
	}
}

func TestBuildSSOutbound(t *testing.T) {
	ob := BuildSSOutbound("4.4.4.4", 8388, "aes-256-gcm", "password")
	if ob["protocol"] != "shadowsocks" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	settings := ob["settings"].(map[string]any)
	servers := settings["servers"].([]any)
	s := servers[0].(map[string]any)
	if s["method"] != "aes-256-gcm" {
		t.Errorf("method = %v", s["method"])
	}
}

func TestBuildStreamSettingsDefaults(t *testing.T) {
	stream := BuildStreamSettings(StreamOpts{})
	if stream["network"] != "tcp" {
		t.Errorf("default network should be tcp, got %v", stream["network"])
	}
	if stream["security"] != "none" {
		t.Errorf("default security should be none, got %v", stream["security"])
	}
}
