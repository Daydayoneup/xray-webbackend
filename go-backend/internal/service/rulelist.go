package service

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// 远程规则列表（分组的订阅来源）：下载后解析成分组的域名 / IP。支持：
//   - gfwlist：GFWList / AdBlock Plus 格式（可为 base64 编码），如 ||example.com、|http://x.com/path、.example.com
//   - clash：Clash 规则集，payload 列表（+.example.com、example.com、1.2.3.0/24）或经典规则（DOMAIN-SUFFIX,example.com）
//   - domains：每行一个域名或 IP/CIDR，也接受 full: / keyword: / regexp: / domain: 前缀
// format 为 auto 时自动识别。

const (
	maxRuleListSize    = 20 << 20
	maxRuleListEntries = 100000
)

var RuleListFormats = map[string]string{"auto": "自动识别", "gfwlist": "GFWList / AdBlock", "clash": "Clash 规则集", "domains": "域名列表"}

// ParsedList 是解析结果。Domains 为分组写法（不带前缀 = 域名后缀），已排序去重，并去掉被上级后缀覆盖的子域名。
type ParsedList struct {
	Domains []string
	IPs     []string
	Skipped int    // 无法表达的条目数（白名单 @@、正则、通配符等）
	Format  string // 实际使用的格式
}

// FetchRuleList 下载规则列表。允许重定向（GitHub/CDN 常见），但每一跳都做内网地址检查。
func FetchRuleList(c *http.Client, rawURL string, allowInternal bool) ([]byte, error) {
	if err := checkListURL(rawURL, allowInternal); err != nil {
		return nil, err
	}
	cc := *c
	cc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("重定向次数过多")
		}
		return checkListURL(req.URL.String(), allowInternal)
	}
	req, _ := http.NewRequest("GET", rawURL, nil)
	req.Header.Set("User-Agent", "xray-panel")
	resp, err := cc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("服务器返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRuleListSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRuleListSize {
		return nil, fmt.Errorf("文件超过 %d MB", maxRuleListSize>>20)
	}
	return data, nil
}

func checkListURL(raw string, allowInternal bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("请填写 http(s) 开头的完整地址")
	}
	if allowInternal {
		return nil
	}
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return fmt.Errorf("无法解析域名 %s", u.Hostname())
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("%s 解析到内网地址 %s，已拒绝", u.Hostname(), ip)
		}
	}
	return nil
}

var (
	listDomainRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)+$`)
	base64Re     = regexp.MustCompile(`^[A-Za-z0-9+/=\s]+$`)
)

// decodeList 处理 base64 编码的列表（GFWList 原始文件就是 base64）。
func decodeList(data []byte) string {
	text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if len(text) > 64 && base64Re.MatchString(text) {
		compact := strings.Join(strings.Fields(text), "")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
			if b, err := enc.DecodeString(compact); err == nil && strings.Contains(string(b), "\n") {
				return string(b)
			}
		}
	}
	return text
}

func detectFormat(text string) string {
	switch {
	case strings.Contains(text, "[AutoProxy") || strings.Contains(text, "\n||") || strings.HasPrefix(text, "||"):
		return "gfwlist"
	case strings.Contains(text, "payload:") || strings.Contains(text, "DOMAIN-SUFFIX,") || strings.Contains(text, "IP-CIDR,"):
		return "clash"
	}
	return "domains"
}

// ParseRuleList 解析下载内容。
func ParseRuleList(data []byte, format string) (*ParsedList, error) {
	text := decodeList(data)
	if format == "" || format == "auto" {
		format = detectFormat(text)
	}
	parse := map[string]func(string) (string, bool){"gfwlist": parseABPLine, "clash": parseClashLine, "domains": parsePlainLine}[format]
	if parse == nil {
		return nil, fmt.Errorf("不支持的格式 %s", format)
	}
	res := &ParsedList{Format: format}
	domains, ips := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") ||
			strings.HasPrefix(line, "//") || line == "payload:" {
			continue
		}
		v, ok := parse(line)
		if !ok {
			res.Skipped++
			continue
		}
		if strings.HasPrefix(v, "ip:") {
			ips[v[3:]] = true
		} else {
			domains[v] = true
		}
	}
	res.Domains = compactDomains(domains)
	for ip := range ips {
		res.IPs = append(res.IPs, ip)
	}
	sort.Strings(res.IPs)
	if len(res.Domains)+len(res.IPs) == 0 {
		return nil, fmt.Errorf("没有解析到任何域名或 IP（按「%s」格式解析），请确认地址和格式", RuleListFormats[format])
	}
	if len(res.Domains)+len(res.IPs) > maxRuleListEntries {
		return nil, fmt.Errorf("条目过多（%d 条，上限 %d）", len(res.Domains)+len(res.IPs), maxRuleListEntries)
	}
	return res, nil
}

// asHostEntry 把主机名转成条目：IP → "ip:x"，合法域名 → 后缀写法。
func asHostEntry(h string) (string, bool) {
	h = strings.ToLower(strings.Trim(h, ". "))
	if ip := net.ParseIP(h); ip != nil {
		return "ip:" + ip.String(), true
	}
	if listDomainRe.MatchString(h) {
		return h, true
	}
	return "", false
}

func cidrEntry(v string) (string, bool) {
	if ip := net.ParseIP(v); ip != nil {
		return "ip:" + ip.String(), true
	}
	if _, n, err := net.ParseCIDR(v); err == nil {
		return "ip:" + n.String(), true
	}
	return "", false
}

// parseABPLine：GFWList / AdBlock 规则。白名单（@@）、正则、带通配符的规则无法用域名表达，跳过。
func parseABPLine(line string) (string, bool) {
	if strings.HasPrefix(line, "@@") || (strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/")) {
		return "", false
	}
	if i := strings.Index(line, "$"); i >= 0 { // 选项，如 $third-party
		line = line[:i]
	}
	line = strings.TrimPrefix(strings.TrimPrefix(line, "||"), "|")
	for _, p := range []string{"http://", "https://"} {
		line = strings.TrimPrefix(line, p)
	}
	line = strings.TrimPrefix(strings.TrimPrefix(line, "*."), ".")
	if i := strings.IndexAny(line, "/^?"); i >= 0 {
		line = line[:i]
	}
	if h, _, err := net.SplitHostPort(line); err == nil {
		line = h
	}
	if strings.Contains(line, "*") {
		return "", false
	}
	return asHostEntry(line)
}

// parseClashLine：payload 列表项（- '+.x'）或经典规则（DOMAIN-SUFFIX,x[,策略]）。
func parseClashLine(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	line = strings.Trim(line, `'"`)
	if typ, rest, ok := strings.Cut(line, ","); ok {
		val, _, _ := strings.Cut(rest, ",")
		val = strings.TrimSpace(val)
		switch strings.ToUpper(strings.TrimSpace(typ)) {
		case "DOMAIN-SUFFIX":
			return asHostEntry(val)
		case "DOMAIN":
			if e, ok := asHostEntry(val); ok && !strings.HasPrefix(e, "ip:") {
				return "full:" + e, true
			}
		case "DOMAIN-KEYWORD":
			if val != "" && !strings.ContainsAny(val, " /") {
				return "keyword:" + strings.ToLower(val), true
			}
		case "IP-CIDR", "IP-CIDR6":
			return cidrEntry(val)
		}
		return "", false
	}
	if e, ok := cidrEntry(line); ok {
		return e, true
	}
	switch {
	case strings.HasPrefix(line, "+."), strings.HasPrefix(line, "*."), strings.HasPrefix(line, "."):
		return asHostEntry(strings.TrimLeft(line, "+*."))
	case strings.Contains(line, "*"):
		return "", false
	}
	if e, ok := asHostEntry(line); ok {
		return "full:" + e, true // Clash domain 规则集中不带前缀的是精确匹配
	}
	return "", false
}

// parsePlainLine：每行一个域名（后缀匹配）或 IP/CIDR；也接受 hosts 格式（0.0.0.0 example.com）与分组前缀写法。
func parsePlainLine(line string) (string, bool) {
	if f := strings.Fields(line); len(f) == 2 && net.ParseIP(f[0]) != nil {
		line = f[1]
	}
	if e, ok := cidrEntry(line); ok {
		return e, true
	}
	if strings.Contains(line, ":") {
		if v, err := NormalizeSetDomain(line); err == nil && v != "" {
			return v, true
		}
		return "", false
	}
	return asHostEntry(strings.TrimLeft(line, "+*."))
}

// compactDomains 排序去重，并去掉已被上级后缀覆盖的条目（有 google.com 时不再需要 www.google.com）。
func compactDomains(set map[string]bool) []string {
	suffix := map[string]bool{}
	for d := range set {
		if !strings.Contains(d, ":") {
			suffix[d] = true
		}
	}
	covered := func(host string, self bool) bool {
		for i := 0; i < len(host); i++ {
			if host[i] == '.' && suffix[host[i+1:]] {
				return true
			}
		}
		return !self && suffix[host]
	}
	out := []string{}
	for d := range set {
		switch {
		case !strings.Contains(d, ":"):
			if covered(d, true) {
				continue
			}
		case strings.HasPrefix(d, "full:"):
			if covered(d[5:], false) {
				continue
			}
		}
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
