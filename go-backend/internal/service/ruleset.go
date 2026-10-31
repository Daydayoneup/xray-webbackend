package service

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"xray-panel/internal/model"
)

var domainEntryPrefixes = []string{"domain:", "full:", "keyword:", "regexp:", "geosite:"}

// NormalizeSetDomain 规范化分组中的一条域名：不带前缀的视为域名后缀，原样保存为 "example.com"；
// 其余保留 full: / keyword: / regexp: / geosite: 前缀。
func NormalizeSetDomain(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	lower := strings.ToLower(s)
	for _, p := range domainEntryPrefixes {
		if !strings.HasPrefix(lower, p) {
			continue
		}
		v := strings.TrimSpace(s[len(p):])
		switch p {
		case "regexp:":
			if _, err := regexp.Compile(v); err != nil || v == "" {
				return "", fmt.Errorf("正则表达式无效: %s", s)
			}
			return p + v, nil
		case "geosite:":
			if !codeRe.MatchString(v) {
				return "", fmt.Errorf("geosite 分类名格式错误: %s", s)
			}
			return p + strings.ToLower(v), nil
		case "domain:":
			p, lower = "", strings.ToLower(v)
		default:
			lower = strings.ToLower(v)
		}
		if err := checkDomain(lower, s); err != nil {
			return "", err
		}
		return p + lower, nil
	}
	lower = strings.TrimPrefix(strings.TrimPrefix(lower, "*."), ".")
	if err := checkDomain(lower, s); err != nil {
		return "", err
	}
	return lower, nil
}

func checkDomain(v, orig string) error {
	if v == "" || strings.ContainsAny(v, "/: \t@") {
		return fmt.Errorf("域名格式错误（不要带协议、端口或路径）: %s", orig)
	}
	return nil
}

// NormalizeSetIP 规范化分组中的一条 IP：IP、CIDR 或 geoip:xx。
func NormalizeSetIP(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(strings.ToLower(s), "geoip:") {
		code := strings.TrimSpace(s[6:])
		if !codeRe.MatchString(code) {
			return "", fmt.Errorf("geoip 分类名格式错误: %s", s)
		}
		return "geoip:" + strings.ToLower(code), nil
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	if _, n, err := net.ParseCIDR(s); err == nil {
		return n.String(), nil
	}
	return "", fmt.Errorf("IP / CIDR 格式错误: %s", s)
}

// NormalizeSetEntries 校验并去重；返回第一个错误。
func NormalizeSetEntries(entries []string, fn func(string) (string, error)) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range entries {
		v, err := fn(e)
		if err != nil {
			return nil, err
		}
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// SetXrayDomains 把分组域名转成 Xray 写法（不带前缀的补 domain:）。
func SetXrayDomains(rs *model.RuleSet) []string {
	out := make([]string, 0, len(rs.Domains))
	for _, d := range rs.Domains {
		has := false
		for _, p := range domainEntryPrefixes {
			if strings.HasPrefix(d, p) {
				has = true
				break
			}
		}
		if !has {
			d = "domain:" + d
		}
		out = append(out, d)
	}
	return out
}

// RuleSetTag 是分组规则的 ruleTag，Describe 用它显示分组名。
func RuleSetTag(rs *model.RuleSet) string { return "分组「" + rs.Name + "」" }

// GeoEntryString 把 geosite 条目转成 Xray 规则写法。
func GeoEntryString(d GeoDomain) string {
	p := map[int]string{0: "keyword:", 1: "regexp:", 2: "domain:", 3: "full:"}[d.Type]
	s := p + d.Value
	for _, a := range d.Attrs {
		s += " @" + a
	}
	return s
}
