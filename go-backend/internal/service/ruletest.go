package service

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RouteTest 是规则测试的结果。
type RouteTest struct {
	Domain   string   `json:"domain,omitempty"`
	IP       string   `json:"ip,omitempty"`
	Port     int      `json:"port"`
	Resolved []string `json:"resolved,omitempty"`
	Matched  bool     `json:"matched"`
	Index    int      `json:"index"`     // 在生成的路由规则中的序号（从 1 开始）
	UserRule int      `json:"user_rule"` // 对应路由页的第几条规则，0 表示非用户规则
	Kind     string   `json:"kind"`      // user | private | default | internal | none
	Match    string   `json:"match"`
	Outbound string   `json:"outbound"`
	IsGroup  bool     `json:"is_group"`
	Steps    []string `json:"steps"`
	Notes    []string `json:"notes,omitempty"`
}

type routeCtx struct {
	domain   string
	ips      []net.IP
	port     int
	network  string
	resolve  func() []net.IP // IPOnDemand 时按需解析
	resolved bool
}

func (c *routeCtx) targetIPs() []net.IP {
	if len(c.ips) == 0 && c.resolve != nil && !c.resolved {
		c.resolved = true
		c.ips = c.resolve()
	}
	return c.ips
}

// ParseTarget 解析用户输入：URL、host:port、域名或 IP。
func ParseTarget(input string) (domain string, ip net.IP, port int, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", nil, 0, fmt.Errorf("请输入域名、IP 或网址")
	}
	port = 443
	if strings.Contains(s, "://") {
		u, e := url.Parse(s)
		if e != nil || u.Hostname() == "" {
			return "", nil, 0, fmt.Errorf("网址格式错误")
		}
		if u.Scheme == "http" {
			port = 80
		}
		if p, e := strconv.Atoi(u.Port()); e == nil {
			port = p
		}
		s = u.Hostname()
	} else if h, p, e := net.SplitHostPort(s); e == nil {
		s = h
		if port, e = strconv.Atoi(p); e != nil {
			return "", nil, 0, fmt.Errorf("端口格式错误")
		}
	}
	s = strings.Trim(s, "[]")
	if ip = net.ParseIP(s); ip != nil {
		return "", ip, port, nil
	}
	return strings.ToLower(strings.TrimSuffix(s, ".")), nil, port, nil
}

func matchDomain(entry, d string, geo *GeoDB, notes *[]string) bool {
	switch {
	case strings.HasPrefix(entry, "domain:"):
		v := entry[7:]
		return d == v || strings.HasSuffix(d, "."+v)
	case strings.HasPrefix(entry, "full:"):
		return d == entry[5:]
	case strings.HasPrefix(entry, "keyword:"):
		return strings.Contains(d, entry[8:])
	case strings.HasPrefix(entry, "regexp:"):
		r := geo.regex(entry[7:])
		return r != nil && r.MatchString(d)
	case strings.HasPrefix(entry, "geosite:"):
		code, attr, _ := strings.Cut(entry[8:], "@")
		list, err := geo.GeoSite(code)
		if err != nil {
			*notes = append(*notes, fmt.Sprintf("无法判断 %s：%v", entry, err))
			return false
		}
		for _, gd := range list {
			if attr != "" && !containsStr(gd.Attrs, attr) {
				continue
			}
			var ok bool
			switch gd.Type {
			case 0:
				ok = strings.Contains(d, gd.Value)
			case 1:
				r := geo.regex(gd.Value)
				ok = r != nil && r.MatchString(d)
			case 2:
				ok = d == gd.Value || strings.HasSuffix(d, "."+gd.Value)
			case 3:
				ok = d == gd.Value
			}
			if ok {
				return true
			}
		}
		return false
	case strings.Contains(entry, ":"):
		return false // ext: 等暂不支持
	default:
		return strings.Contains(d, entry)
	}
}

func matchIP(entry string, ip net.IP, geo *GeoDB, notes *[]string) bool {
	if strings.HasPrefix(entry, "geoip:") {
		code := entry[6:]
		neg := strings.HasPrefix(code, "!")
		code = strings.TrimPrefix(code, "!")
		nets, err := geo.GeoIP(code)
		if err != nil && strings.EqualFold(code, "private") {
			nets, err = parseCIDRs(PrivateCIDRs), nil
		}
		if err != nil {
			*notes = append(*notes, fmt.Sprintf("无法判断 %s：%v", entry, err))
			return false
		}
		hit := false
		for _, n := range nets {
			if n.Contains(ip) {
				hit = true
				break
			}
		}
		return hit != neg
	}
	if _, n, err := net.ParseCIDR(entry); err == nil {
		return n.Contains(ip)
	}
	if e := net.ParseIP(entry); e != nil {
		return e.Equal(ip)
	}
	return false
}

func parseCIDRs(xs []string) []*net.IPNet {
	var out []*net.IPNet
	for _, x := range xs {
		if _, n, err := net.ParseCIDR(x); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func matchPort(spec any, port int) bool {
	s := fmt.Sprint(spec)
	if f, ok := spec.(float64); ok {
		s = strconv.Itoa(int(f))
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		lo, hi, isRange := strings.Cut(part, "-")
		a, _ := strconv.Atoi(lo)
		b := a
		if isRange {
			b, _ = strconv.Atoi(hi)
		}
		if port >= a && port <= b {
			return true
		}
	}
	return false
}

func ruleApplies(r map[string]any, c *routeCtx, geo *GeoDB, notes *[]string) bool {
	if len(strList(r["inboundTag"])) > 0 {
		return false // 内部规则（metrics/DNS），不作用于用户流量
	}
	if ds := strList(r["domain"]); len(ds) > 0 {
		if c.domain == "" {
			return false
		}
		hit := false
		for _, e := range ds {
			if matchDomain(e, c.domain, geo, notes) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if es := strList(r["ip"]); len(es) > 0 {
		ips := c.targetIPs()
		hit := false
		for _, ip := range ips {
			for _, e := range es {
				if matchIP(e, ip, geo, notes) {
					hit = true
					break
				}
			}
		}
		if !hit {
			return false
		}
	}
	if p, ok := r["port"]; ok && !matchPort(p, c.port) {
		return false
	}
	if n, ok := r["network"].(string); ok && !strings.Contains(n, c.network) {
		return false
	}
	return true
}

// TestRoute 在生成的配置上模拟 Xray 的路由匹配（含 domainStrategy 的两轮匹配语义）。
// resolve 为 nil 时使用系统 DNS。
func TestRoute(cfg map[string]any, ruleSource []int, geo *GeoDB, input string, resolve func(string) ([]net.IP, error)) (*RouteTest, error) {
	domain, ip, port, err := ParseTarget(input)
	if err != nil {
		return nil, err
	}
	if resolve == nil {
		resolve = func(h string) ([]net.IP, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, h)
			var out []net.IP
			for _, a := range addrs {
				out = append(out, a.IP)
			}
			return out, err
		}
	}
	routing, _ := cfg["routing"].(map[string]any)
	rules := toMaps(routing["rules"])
	strategy, _ := routing["domainStrategy"].(string)
	res := &RouteTest{Domain: domain, Port: port, Kind: "none"}
	if ip != nil {
		res.IP = ip.String()
	}
	c := &routeCtx{domain: domain, port: port, network: "tcp"}
	if ip != nil {
		c.ips = []net.IP{ip}
	}
	doResolve := func() []net.IP {
		ips, err := resolve(domain)
		if err != nil || len(ips) == 0 {
			res.Steps = append(res.Steps, fmt.Sprintf("解析 %s 失败：%v", domain, err))
			return nil
		}
		for _, x := range ips {
			res.Resolved = append(res.Resolved, x.String())
		}
		res.Steps = append(res.Steps, fmt.Sprintf("解析 %s → %s", domain, strings.Join(res.Resolved, ", ")))
		return ips
	}
	if strategy == "IPOnDemand" && domain != "" {
		c.resolve = doResolve
	}

	pass := func() bool {
		for i, r := range rules {
			if ruleApplies(r, c, geo, &res.Notes) {
				res.fill(i, r, ruleSource)
				return true
			}
		}
		return false
	}
	target := domain
	if target == "" {
		target = res.IP
	}
	res.Steps = append(res.Steps, fmt.Sprintf("按 %s:%d 匹配规则（域名策略 %s）", target, port, selStr(strategy, "AsIs")))
	if pass() {
		return res, nil
	}
	if strategy == "IPIfNonMatch" && domain != "" {
		res.Steps = append(res.Steps, "没有规则按域名命中，解析成 IP 再匹配一轮")
		c.ips = doResolve()
		if len(c.ips) > 0 && pass() {
			return res, nil
		}
	}
	res.Steps = append(res.Steps, "没有规则命中，使用第一个出站")
	if obs := toMaps(cfg["outbounds"]); len(obs) > 0 {
		res.Outbound, _ = obs[0]["tag"].(string)
	}
	res.Kind, res.Match = "default", "没有规则命中（第一个出站）"
	return res, nil
}

func (res *RouteTest) fill(i int, r map[string]any, ruleSource []int) {
	res.Matched, res.Index, res.Match = true, i+1, Describe(r)
	if t, _ := r["balancerTag"].(string); t != "" {
		res.Outbound, res.IsGroup = t, true
	} else {
		res.Outbound, _ = r["outboundTag"].(string)
	}
	switch {
	case res.Match == "默认出口(其余流量)":
		res.Kind = "default"
	case res.Match == "内网地址":
		res.Kind = "private"
	default:
		res.Kind = "user"
		if i < len(ruleSource) {
			res.UserRule = ruleSource[i]
		}
	}
	res.Steps = append(res.Steps, fmt.Sprintf("命中第 %d 条：%s", res.Index, res.Match))
}

func toMaps(v any) []map[string]any {
	var out []map[string]any
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = x
	}
	return out
}
