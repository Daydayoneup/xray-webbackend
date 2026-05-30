package service

import (
	"encoding/base64"
	"testing"
)

func b64encodeGo(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestB64Decode(t *testing.T) {
	result := b64decode("dGVzdA==")
	if string(result) != "test" {
		t.Errorf("b64decode = %s, want test", string(result))
	}
}

func TestExtractLinksBase64(t *testing.T) {
	content := "dm1lc3M6Ly9saW5rMQp2bWVzczovL2xpbmsy"
	links, _ := ExtractLinks(content)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestExtractLinksPlain(t *testing.T) {
	content := "vmess://test1\nSTATUS=ok\nvless://test2"
	links, meta := ExtractLinks(content)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if meta["STATUS"] != "ok" {
		t.Errorf("STATUS = %s", meta["STATUS"])
	}
}

func TestParseVMess(t *testing.T) {
	raw := `{"v":"2","ps":"TestNode","add":"1.2.3.4","port":"443","id":"uuid","aid":"0","net":"ws","type":"none","host":"","path":"/ws","tls":"tls","sni":"1.2.3.4"}`
	b64 := b64encodeGo(raw)
	link := "vmess://" + b64
	node, err := parsers["vmess"](link)
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "TestNode" {
		t.Errorf("name = %s, want TestNode", node.Name)
	}
	if node.Host != "1.2.3.4" {
		t.Errorf("host = %s", node.Host)
	}
	stream := node.Outbound["streamSettings"].(map[string]any)
	if stream["security"] != "tls" {
		t.Errorf("security = %v", stream["security"])
	}
}

func TestAssignTags(t *testing.T) {
	nodes := []NodeRaw{
		{Name: "a", Host: "1.1.1.1", Port: 443, Type: "vmess", Outbound: map[string]any{}},
		{Name: "b", Host: "2.2.2.2", Port: 443, Type: "vless", Outbound: map[string]any{}},
	}
	AssignTags(nodes)
	if nodes[0].Tag != "node-0" {
		t.Errorf("tag[0] = %s", nodes[0].Tag)
	}
	if nodes[1].Tag != "node-1" {
		t.Errorf("tag[1] = %s", nodes[1].Tag)
	}
}

func TestSkipUnsupported(t *testing.T) {
	links := []string{"ssr://dGVzdA==", "hysteria://test", "invalid://test"}
	nodes, skipped := ParseLinks(links)
	if len(skipped) < 2 {
		t.Errorf("expected >= 2 skipped, got %d", len(skipped))
	}
	_ = nodes
}
