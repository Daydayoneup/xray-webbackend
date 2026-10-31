package service

import (
	"net"
	"strings"
	"testing"

	"xray-panel/internal/model"
)

func TestNormalizeSetEntries(t *testing.T) {
	ok := map[string]string{
		"Example.COM": "example.com", "*.foo.com": "foo.com", ".bar.com": "bar.com", "domain:Baz.com": "baz.com",
		"full:www.a.com": "full:www.a.com", "keyword:Google": "keyword:google", "regexp:^a\\d+\\.com$": "regexp:^a\\d+\\.com$",
		"geosite:Netflix": "geosite:netflix",
	}
	for in, want := range ok {
		if got, err := NormalizeSetDomain(in); err != nil || got != want {
			t.Errorf("domain %q → %q %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://a.com", "a.com/path", "a.com:443", "regexp:(", "geosite:a b"} {
		if _, err := NormalizeSetDomain(bad); err == nil {
			t.Errorf("domain %q should be rejected", bad)
		}
	}
	for in, want := range map[string]string{"8.8.8.8": "8.8.8.8", "10.1.2.3/8": "10.0.0.0/8", "GeoIP:CN": "geoip:cn", "2001:db8::1": "2001:db8::1"} {
		if got, err := NormalizeSetIP(in); err != nil || got != want {
			t.Errorf("ip %q → %q %v", in, got, err)
		}
	}
	if _, err := NormalizeSetIP("example.com"); err == nil {
		t.Error("domain in IP list should be rejected")
	}
	got, _ := NormalizeSetEntries([]string{"a.com", " ", "A.com", "b.com"}, NormalizeSetDomain)
	if strings.Join(got, ",") != "a.com,b.com" {
		t.Errorf("dedupe = %v", got)
	}
}

func TestBuildRuleSet(t *testing.T) {
	geo := NewGeoDB(writeGeo(t))
	st := &model.PanelState{
		Nodes: []model.Node{{Tag: "node-a", Name: "A", Spec: testSpec}},
		RuleSets: []model.RuleSet{
			{Tag: "set-0", Name: "工作", Domains: []string{"corp.com", "geosite:netflix"}, IPs: []string{"10.8.0.0/16", "geoip:us"}},
			{Tag: "set-1", Name: "空"},
		},
		Rules: []model.Rule{
			{Type: "set", Value: "set-0", Outbound: "node-a", Enabled: true},
			{Type: "set", Value: "set-1", Outbound: "node-a", Enabled: true},
			{Type: "set", Value: "set-9", Outbound: "node-a", Enabled: true},
		},
		DefaultOutbound: "direct",
	}
	res := Build(st, BuildOpts{})
	var setRules []map[string]any
	for i, r := range res.Config["routing"].(map[string]any)["rules"].([]any) {
		m := r.(map[string]any)
		if m["ruleTag"] != nil {
			setRules = append(setRules, m)
			if res.RuleSource[i] != 1 {
				t.Errorf("rule source = %d", res.RuleSource[i])
			}
		}
	}
	if len(setRules) != 2 || setRules[0]["ip"] != nil || setRules[1]["domain"] != nil {
		t.Fatalf("set must expand to separate domain and ip rules: %v", setRules)
	}
	if d := setRules[0]["domain"].([]string); d[0] != "domain:corp.com" || d[1] != "geosite:netflix" {
		t.Errorf("domains = %v", d)
	}
	if Describe(setRules[0]) != "分组「工作」 · 域名" || Describe(setRules[1]) != "分组「工作」 · IP" {
		t.Errorf("describe = %q / %q", Describe(setRules[0]), Describe(setRules[1]))
	}
	w := strings.Join(res.Warnings, "\n")
	if !strings.Contains(w, "是空的") || !strings.Contains(w, "已不存在") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	resolve := func(h string) ([]net.IP, error) {
		if h == "us-site.com" {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	}
	for in, want := range map[string]int{"www.corp.com": 1, "netflix.com": 1, "10.8.1.1": 1, "us-site.com": 1, "other.com": 0} {
		r, _ := TestRoute(res.Config, res.RuleSource, geo, in, resolve)
		if r.UserRule != want {
			t.Errorf("%s → rule %d, want %d (%v)", in, r.UserRule, want, r.Steps)
		}
	}
}

func TestGeoCatalog(t *testing.T) {
	g := NewGeoDB(writeGeo(t))
	codes, err := g.Catalog("site")
	if err != nil || len(codes) != 2 || codes[0].Code != "cn" || codes[1].Count != 1 {
		t.Fatalf("catalog = %v %v", codes, err)
	}
	es, total, err := g.Entries("site", "netflix", "", 10)
	if err != nil || total != 1 || es[0] != "domain:netflix.com" {
		t.Errorf("entries = %v %d %v", es, total, err)
	}
	es, total, _ = g.Entries("ip", "us", "8.8", 10)
	if total != 1 || es[0] != "8.8.8.0/24" {
		t.Errorf("ip entries = %v", es)
	}
	if _, err := NewGeoDB(t.TempDir()).Catalog("ip"); err == nil {
		t.Error("missing file should error")
	}
}

func TestNamedRules(t *testing.T) {
	geo := NewGeoDB(writeGeo(t))
	st := &model.PanelState{
		Nodes:    []model.Node{{Tag: "node-a", Name: "A", Spec: testSpec}},
		RuleSets: []model.RuleSet{{Tag: "set-0", Name: "工作", Domains: []string{"corp.com"}}},
		Rules: []model.Rule{
			{Name: "Netflix 走 A", Type: "geosite", Value: "netflix", Outbound: "node-a", Enabled: true},
			{Name: "公司", Type: "set", Value: "set-0", Outbound: "direct", Enabled: true},
			{Type: "port", Value: "25", Outbound: "block", Enabled: true},
		},
		DefaultOutbound: "direct",
	}
	res := Build(st, BuildOpts{})
	var descs []string
	for i, r := range res.Config["routing"].(map[string]any)["rules"].([]any) {
		if res.RuleSource[i] > 0 {
			descs = append(descs, Describe(r.(map[string]any)))
		}
	}
	want := []string{"Netflix 走 A（预置集合 netflix）", "公司 · 分组「工作」 · 域名", "端口 25"}
	if strings.Join(descs, "|") != strings.Join(want, "|") {
		t.Errorf("describe = %q", descs)
	}
	r, _ := TestRoute(res.Config, res.RuleSource, geo, "netflix.com", func(string) ([]net.IP, error) { return nil, nil })
	if r.UserRule != 1 || r.Match != want[0] {
		t.Errorf("route test match = %d %q", r.UserRule, r.Match)
	}
}

// 新版 Xray 拒绝重复的 ruleTag：分组规则拆出的两条、同名的规则都必须得到不同的 ruleTag。
func TestRuleTagsUnique(t *testing.T) {
	st := &model.PanelState{
		Nodes:    []model.Node{{Tag: "node-a", Name: "A", Spec: testSpec}},
		RuleSets: []model.RuleSet{{Tag: "set-0", Name: "GFWList", Domains: []string{"google.com"}, IPs: []string{"8.8.8.8/32"}}},
		Rules: []model.Rule{
			{Name: "gfw", Type: "set", Value: "set-0", Outbound: "node-a", Enabled: true},
			{Name: "同名", Type: "domain-suffix", Value: "a.com", Outbound: "node-a", Enabled: true},
			{Name: "同名", Type: "domain-suffix", Value: "b.com", Outbound: "direct", Enabled: true},
		},
		DefaultOutbound: "direct",
	}
	res := Build(st, BuildOpts{})
	seen := map[string]bool{}
	var tags []string
	for _, x := range res.Config["routing"].(map[string]any)["rules"].([]any) {
		r := x.(map[string]any)
		tag, _ := r["ruleTag"].(string)
		if tag == "" {
			continue
		}
		if seen[tag] {
			t.Fatalf("duplicate ruleTag %q in %v", tag, tags)
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	want := []string{"gfw · 分组「GFWList」 · 域名", "gfw · 分组「GFWList」 · IP", "同名", "同名 #3"}
	if strings.Join(tags, "|") != strings.Join(want, "|") {
		t.Fatalf("tags = %q", tags)
	}
	if d := Describe(map[string]any{"ruleTag": tags[1], "ip": []string{"8.8.8.8/32"}}); d != "gfw · 分组「GFWList」 · IP" {
		t.Errorf("Describe = %q", d)
	}
	// 旧版本生成的配置（无后缀）仍然按类型补上
	if d := Describe(map[string]any{"ruleTag": "分组「GFWList」", "domain": []string{"domain:google.com"}}); d != "分组「GFWList」 · 域名" {
		t.Errorf("legacy Describe = %q", d)
	}
}
