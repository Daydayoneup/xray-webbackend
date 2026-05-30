package service

import "testing"

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
