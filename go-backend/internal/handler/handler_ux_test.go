package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"xray-panel/internal/model"
)

func authed(t *testing.T) (*httptest.Server, func(method, path string, body any) (*http.Response, map[string]any)) {
	t.Helper()
	return authedWith(t, setupTestApp(t))
}

func authedWith(t *testing.T, srv *Server) (*httptest.Server, func(method, path string, body any) (*http.Response, map[string]any)) {
	t.Helper()
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	lb, _ := json.Marshal(map[string]string{"password": "testpw"})
	resp, _ := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(lb))
	var lr map[string]any
	json.NewDecoder(resp.Body).Decode(&lr)
	token := lr["token"].(string)
	do := func(method, path string, body any) (*http.Response, map[string]any) {
		var rd *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}
	return ts, do
}

// 问题 7：手动创建的 vmess 代理，编辑时必须能拿回全部参数。
func TestManualProxyKeepsFieldsForEdit(t *testing.T) {
	_, do := authed(t)
	resp, px := do("POST", "/api/proxies", map[string]any{
		"name": "落地", "protocol": "vmess", "host": "v.com", "port": 443, "uuid": "uid-1",
		"network": "ws", "tls": "tls", "sni": "s.com", "path": "/ws", "ws_host": "h.com", "fingerprint": "chrome",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, px)
	}
	spec := px["spec"].(map[string]any)
	for k, want := range map[string]string{"uuid": "uid-1", "network": "ws", "security": "tls", "sni": "s.com",
		"path": "/ws", "host_header": "h.com", "fingerprint": "chrome"} {
		if spec[k] != want {
			t.Errorf("spec[%s] = %v, want %s", k, spec[k], want)
		}
	}
}

func TestRoutingRejectsBadRuleValue(t *testing.T) {
	_, do := authed(t)
	resp, out := do("PUT", "/api/routing", map[string]any{
		"default_outbound": "direct",
		"rules":            []map[string]any{{"type": "port", "value": "abc", "outbound": "direct", "enabled": true}},
	})
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400 (%v)", resp.StatusCode, out)
	}
	resp, out = do("PUT", "/api/routing", map[string]any{
		"default_outbound": "direct", "domain_strategy": "AsIs",
		"rules": []map[string]any{{"type": "geoip", "value": "cn, jp", "outbound": "direct", "enabled": true}},
	})
	if resp.StatusCode != 200 || out["domain_strategy"] != "AsIs" {
		t.Fatalf("status = %d %v", resp.StatusCode, out)
	}
}

func TestStatusReportsWarningsAndApplyWorksWithoutNodes(t *testing.T) {
	_, do := authed(t)
	_, st := do("GET", "/api/xray/status", nil)
	if st["dirty"] != true || st["warnings"] == nil {
		t.Errorf("status = %v", st)
	}
	resp, out := do("POST", "/api/apply", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("apply without nodes = %d %v", resp.StatusCode, out)
	}
}

func TestCreateEncryptedInboundAndShare(t *testing.T) {
	_, do := authed(t)
	for _, n := range []string{"alice", "bob"} {
		if resp, out := do("POST", "/api/users", map[string]any{"name": n}); resp.StatusCode != 201 {
			t.Fatalf("create user = %d %v", resp.StatusCode, out)
		}
	}
	resp, ib := do("POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 8443, "listen": "0.0.0.0",
		"users": []string{"alice", "bob"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create vless = %d %v", resp.StatusCode, ib)
	}
	r := ib["reality"].(map[string]any)
	if r["private_key"] == "" || r["public_key"] == "" || len(ib["users"].([]any)) != 2 {
		t.Fatalf("keys/users should be generated: %v", ib)
	}
	resp, out := do("GET", "/api/inbounds/"+ib["tag"].(string)+"/share?host=1.2.3.4", nil)
	items := out["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 2 || !strings.HasPrefix(items[0].(map[string]any)["link"].(string), "vless://") {
		t.Fatalf("share = %d %v", resp.StatusCode, out)
	}
	resp, out = do("POST", "/api/inbounds", map[string]any{"protocol": "shadowsocks", "port": 8443})
	if resp.StatusCode != 400 {
		t.Errorf("duplicate port should be rejected: %v", out)
	}
	resp, out = do("POST", "/api/inbounds", map[string]any{"protocol": "socks", "port": 10085})
	if resp.StatusCode != 201 { // 测试环境未开启 metrics，10085 可用
		t.Errorf("socks create = %d %v", resp.StatusCode, out)
	}
}

func TestUsersAPI(t *testing.T) {
	_, do := authed(t)
	_, ib := do("POST", "/api/inbounds", map[string]any{"protocol": "socks", "port": 7001})
	tag := ib["tag"].(string)
	resp, u := do("POST", "/api/users", map[string]any{"name": "dave", "inbounds": []string{tag}})
	if resp.StatusCode != 201 || u["uuid"] == "" || u["password"] == "" || u["ss_key"] == "" {
		t.Fatalf("create = %d %v", resp.StatusCode, u)
	}
	if resp, _ := do("POST", "/api/users", map[string]any{"name": "dave"}); resp.StatusCode != 400 {
		t.Error("duplicate name should be rejected")
	}
	if resp, _ := do("POST", "/api/users", map[string]any{"name": "a b"}); resp.StatusCode != 400 {
		t.Error("invalid name should be rejected")
	}
	resp, out := do("GET", "/api/users/dave/share?host=1.2.3.4", nil)
	if items := out["items"].([]any); resp.StatusCode != 200 || len(items) != 1 {
		t.Fatalf("share = %v", out)
	}
	// 入站编辑页取消关联
	if resp, out := do("PUT", "/api/inbounds/"+tag, map[string]any{"protocol": "socks", "port": 7001, "users": []string{},
		"dns": map[string]any{"server": "8.8.8.8"}}); resp.StatusCode != 200 || out["dns"].(map[string]any)["server"] != "8.8.8.8:53" {
		t.Fatalf("update inbound = %d %v", resp.StatusCode, out)
	}
	if _, out := do("GET", "/api/users/dave/share?host=1.2.3.4", nil); len(out["items"].([]any)) != 0 {
		t.Errorf("link should be removed: %v", out)
	}
	if resp, out := do("PUT", "/api/inbounds/"+tag, map[string]any{"protocol": "socks", "port": 7001, "dns": map[string]any{"server": "dns.google"}}); resp.StatusCode != 400 {
		t.Errorf("invalid dns should be rejected: %v", out)
	}
	if resp, _ := do("DELETE", "/api/users/dave", nil); resp.StatusCode != 200 {
		t.Error("delete failed")
	}
}

func TestRuleSetsAPI(t *testing.T) {
	_, do := authed(t)
	resp, rs := do("POST", "/api/rulesets", map[string]any{"name": "工作", "domains": []string{"Corp.com", "*.corp.com"}, "ips": []string{"10.0.0.0/8"}})
	if resp.StatusCode != 201 || len(rs["domains"].([]any)) != 1 {
		t.Fatalf("create = %d %v", resp.StatusCode, rs)
	}
	tag := rs["tag"].(string)
	if resp, _ := do("POST", "/api/rulesets", map[string]any{"name": "工作"}); resp.StatusCode != 400 {
		t.Error("duplicate name should be rejected")
	}
	if resp, out := do("POST", "/api/rulesets", map[string]any{"name": "坏", "ips": []string{"a.com"}}); resp.StatusCode != 400 {
		t.Errorf("invalid ip should be rejected: %v", out)
	}
	rules := []map[string]any{{"name": "  公司直连 ", "type": "set", "value": tag, "outbound": "direct", "enabled": true}}
	resp, out := do("PUT", "/api/routing", map[string]any{"default_outbound": "direct", "rules": rules})
	if resp.StatusCode != 200 || out["rules"].([]any)[0].(map[string]any)["name"] != "公司直连" {
		t.Fatalf("routing = %d %v", resp.StatusCode, out)
	}

	bad := []map[string]any{{"type": "set", "value": "set-99", "outbound": "direct", "enabled": true}}
	if resp, _ := do("PUT", "/api/routing", map[string]any{"default_outbound": "direct", "rules": bad}); resp.StatusCode != 400 {
		t.Error("unknown set should be rejected")
	}
	resp, out = do("DELETE", "/api/rulesets/"+tag, nil)
	if resp.StatusCode != 200 || out["removed_rules"].(float64) != 1 {
		t.Errorf("delete = %v", out)
	}
	if _, out := do("GET", "/api/geo/catalog?kind=site", nil); out["available"] != false {
		t.Errorf("catalog without geo files = %v", out)
	}
}

func TestRemoteRuleSet(t *testing.T) {
	srv := setupTestApp(t)
	gfw := base64.StdEncoding.EncodeToString([]byte("[AutoProxy 0.2.9]\n! comment\n||google.com\n||www.google.com\n|http://85.17.73.31/\n@@||cn.example.com\n.twitter.com\n"))
	calls, fail := 0, false
	srv.App.SetListFetcher(func(u string) ([]byte, error) {
		calls++
		if fail {
			return nil, fmt.Errorf("timeout")
		}
		return []byte(gfw), nil
	})
	_, do := authedWith(t, srv)
	resp, rs := do("POST", "/api/rulesets", map[string]any{"name": "GFWList",
		"source": map[string]any{"url": "https://example.com/gfwlist.txt", "format": "auto", "update_hours": 24}})
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d %v", resp.StatusCode, rs)
	}
	src := rs["source"].(map[string]any)
	if rs["domain_count"].(float64) != 2 || rs["ip_count"].(float64) != 1 || src["detected"] != "gfwlist" || src["skipped"].(float64) != 1 {
		t.Fatalf("parsed = %v", rs)
	}
	tag := rs["tag"].(string)
	// 只改名称/更新间隔：不重新下载
	if resp, out := do("PUT", "/api/rulesets/"+tag, map[string]any{"name": "GFW",
		"source": map[string]any{"url": "https://example.com/gfwlist.txt", "format": "auto", "update_hours": 12}}); resp.StatusCode != 200 || calls != 1 || out["domain_count"].(float64) != 2 {
		t.Fatalf("rename = %d calls=%d %v", resp.StatusCode, calls, out)
	}
	// 更新失败：返回错误，保留原内容
	fail = true
	if resp, out := do("POST", "/api/rulesets/"+tag+"/refresh", nil); resp.StatusCode != 400 || !strings.Contains(fmt.Sprint(out), "timeout") {
		t.Errorf("refresh failure = %d %v", resp.StatusCode, out)
	}
	_, out := do("GET", "/api/rulesets/"+tag+"/entries?q=google", nil)
	if out["total"].(float64) != 1 || out["entries"].([]any)[0] != "google.com" {
		t.Errorf("entries = %v", out)
	}
	// 下载失败时新建会被拒绝
	if resp, _ := do("POST", "/api/rulesets", map[string]any{"name": "坏", "source": map[string]any{"url": "https://example.com/x"}}); resp.StatusCode != 400 {
		t.Error("create with failing source should be rejected")
	}
}

func TestRuleSetViewRefNames(t *testing.T) {
	st := &model.PanelState{
		RuleSets: []model.RuleSet{{Tag: "set-0", Name: "工作", Domains: []string{"a.com"}}},
		Rules: []model.Rule{
			{Type: "domain-suffix", Value: "x.com"},
			{Name: "公司直连", Type: "set", Value: "set-0"},
			{Type: "set", Value: "set-0"},
		},
	}
	v := newRuleSetView(st, st.RuleSets[0])
	if len(v.RefRules) != 2 || v.RefRules[0] != (ruleRef{2, "公司直连"}) || v.RefRules[1] != (ruleRef{3, ""}) {
		t.Errorf("ref rules = %+v", v.RefRules)
	}
}

func TestFreshStateRoutingRulesArray(t *testing.T) {
	_, do := authed(t)
	_, body := do("GET", "/api/routing", nil)
	if _, ok := body["rules"].([]any); !ok {
		t.Fatalf("rules should be an array on a fresh install, got %#v", body["rules"])
	}
}

func TestStatusPasswordAndInstall(t *testing.T) {
	_, do := authed(t)
	_, body := do("GET", "/api/xray/status", nil)
	if body["password_initial"] != false {
		t.Fatalf("PANEL_PASSWORD should not be initial: %v", body["password_initial"])
	}
	if _, ok := body["install"].(map[string]any); !ok {
		t.Fatalf("install missing: %v", body)
	}
}

func TestRoutingRejectsDuplicateRuleNames(t *testing.T) {
	_, do := authed(t)
	resp, body := do("PUT", "/api/routing", map[string]any{
		"default_outbound": "direct",
		"rules": []map[string]any{
			{"name": "gfw", "type": "domain-suffix", "value": "a.com", "outbound": "direct", "enabled": true},
			{"name": "gfw", "type": "domain-suffix", "value": "b.com", "outbound": "direct", "enabled": true},
		},
	})
	if resp.StatusCode != 400 || !strings.Contains(fmt.Sprint(body["detail"]), "重复") {
		t.Fatalf("want 400 duplicate name, got %d %v", resp.StatusCode, body)
	}
}
