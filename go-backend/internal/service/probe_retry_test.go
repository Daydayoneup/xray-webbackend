package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xray-panel/internal/model"
)

// 订阅里的异常节点（如 ss 加密方式 none）会让测速用的 Xray 整体启动失败：应去掉该出站后重试。
func TestProbeRetriesWithoutRejectedOutbound(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "xray")
	script := `#!/bin/sh
cfg="$2"
cat "$cfg" >> ` + log + `
echo >> ` + log + `
if grep -q '"method":"none"' "$cfg"; then
  echo "Failed to start: main: failed to load config files: [$cfg] > infra/conf: failed to build outbound config with tag probe-out-1 > infra/conf: unknown cipher method: none"
  exit 1
fi
echo "Failed to start: something else"
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	good := BuildOutbound(&model.OutboundSpec{Protocol: "trojan", Host: "a.example", Port: 443, UUID: "pw", Security: "tls"})
	bad := map[string]any{"protocol": "shadowsocks", "settings": map[string]any{"servers": []any{
		map[string]any{"address": "1.1.1.1", "port": 443, "method": "none", "password": "x"}}}}
	obs := []map[string]any{good, bad}
	_, err := probeOutbounds(bin, dir, obs, time.Second)
	if err == nil || !strings.Contains(err.Error(), "something else") {
		t.Fatalf("want second-attempt error, got %v", err)
	}
	calls := strings.Split(strings.TrimSpace(readFile(t, log)), "\n")
	if len(calls) != 2 {
		t.Fatalf("want 2 attempts, got %d", len(calls))
	}
	if strings.Contains(calls[1], `"method":"none"`) || !strings.Contains(calls[1], "probe-out-0") {
		t.Fatalf("second attempt should drop only the rejected outbound: %s", calls[1])
	}
	if obs[1] == nil {
		t.Fatal("caller's slice must not be modified")
	}
}

func TestUnsupportedSSMethod(t *testing.T) {
	spec := &model.OutboundSpec{Protocol: "shadowsocks", Host: "1.1.1.1", Port: 443, Method: "none", UUID: "x"}
	if err := ValidateSpec(spec); err == nil || !strings.Contains(err.Error(), "不支持的加密方式") {
		t.Fatalf("none should be rejected, got %v", err)
	}
	if n := (&model.Node{Tag: "n1", Spec: spec}); NodeOutbound(n) != nil {
		t.Fatal("node with unsupported cipher should be invalid")
	}
	spec.Method = "chacha20-ietf-poly1305"
	if err := ValidateSpec(spec); err != nil {
		t.Fatal(err)
	}
	// 订阅解析：信息节点被跳过并给出原因
	link := "ss://" + b64encodeGo("none:x") + "@1.1.1.1:443#%E7%BD%91%E7%AB%99%E8%AE%BF%E9%97%AE%E5%9C%B0%E5%9D%80"
	nodes, skipped := ParseLinks([]string{link})
	if len(nodes) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Detail, "none") {
		t.Fatalf("nodes=%d skipped=%+v", len(nodes), skipped)
	}
}

func readFile(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestXrayFingerprint(t *testing.T) {
	cases := map[string]string{
		"": "", "chrome": "chrome", "Chrome140": "chrome", "firefox_120": "firefox", "safari": "safari",
		"hellochrome_120": "hellochrome_120", "weird": "chrome", "360": "360",
	}
	for in, want := range cases {
		if got := XrayFingerprint(in); got != want {
			t.Errorf("XrayFingerprint(%q) = %q, want %q", in, got, want)
		}
	}
	ob := BuildOutbound(&model.OutboundSpec{Protocol: "trojan", Host: "a.example", Port: 443, UUID: "pw", Security: "tls", Fingerprint: "chrome140"})
	ts := ob["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
	if ts["fingerprint"] != "chrome" {
		t.Fatalf("fingerprint = %v", ts["fingerprint"])
	}
}
