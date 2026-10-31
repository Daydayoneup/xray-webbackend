package service

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseGFWList(t *testing.T) {
	raw := strings.Join([]string{
		"[AutoProxy 0.2.9]", "! Checksum: x", "||google.com", "||mail.google.com", ".twitter.com",
		"|http://www.example.org/path?x=1", "|https://*.wildcard.com", "@@||whitelisted.cn", "/^https?:\\/\\/[^\\/]+blogspot\\.(.*)/",
		"|http://85.17.73.31/", "facebook.com", "||ads.example.net^$third-party", "keyword-only",
	}, "\n")
	for _, data := range [][]byte{[]byte(raw), []byte(base64.StdEncoding.EncodeToString([]byte(raw)))} {
		p, err := ParseRuleList(data, "auto")
		if err != nil {
			t.Fatal(err)
		}
		want := "ads.example.net,facebook.com,google.com,twitter.com,wildcard.com,www.example.org" // *.x → x 后缀
		if p.Format != "gfwlist" || strings.Join(p.Domains, ",") != want || strings.Join(p.IPs, ",") != "85.17.73.31" || p.Skipped != 3 /* @@白名单、正则、无点关键字 */ {
			t.Errorf("format=%s domains=%v ips=%v skipped=%d", p.Format, p.Domains, p.IPs, p.Skipped)
		}
	}
}

func TestParseClashAndPlain(t *testing.T) {
	clash := "payload:\n  - '+.google.com'\n  - 'www.google.com'\n  - \"exact.example.com\"\n  - '91.108.4.0/22'\n  - '*.weird'\n"
	p, err := ParseRuleList([]byte(clash), "auto")
	if err != nil || p.Format != "clash" || strings.Join(p.Domains, ",") != "full:exact.example.com,google.com" || p.IPs[0] != "91.108.4.0/22" {
		t.Errorf("clash payload = %+v %v", p, err)
	}
	classic := "DOMAIN-SUFFIX,netflix.com,Proxy\nDOMAIN,api.x.com\nDOMAIN-KEYWORD,openai\nIP-CIDR,8.8.8.0/24,no-resolve\nPROCESS-NAME,foo\n"
	p, err = ParseRuleList([]byte(classic), "auto")
	if err != nil || strings.Join(p.Domains, ",") != "full:api.x.com,keyword:openai,netflix.com" || p.IPs[0] != "8.8.8.0/24" || p.Skipped != 1 {
		t.Errorf("clash classic = %+v %v", p, err)
	}
	plain := "# list\nexample.com\nsub.example.com\n0.0.0.0 ads.tracker.com\n10.0.0.0/8\nfull:a.b.com\nnot a domain\n"
	p, err = ParseRuleList([]byte(plain), "auto")
	if err != nil || p.Format != "domains" || strings.Join(p.Domains, ",") != "ads.tracker.com,example.com,full:a.b.com" || p.IPs[0] != "10.0.0.0/8" {
		t.Errorf("plain = %+v %v", p, err)
	}
	if _, err := ParseRuleList([]byte("<html>not found</html>"), "auto"); err == nil {
		t.Error("garbage should be rejected")
	}
}
