package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"xray-panel/internal/model"
)

var (
	DefaultDomesticDNS = []string{"223.5.5.5", "119.29.29.29"}
	DefaultRemoteDNS   = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	DNSModes           = []string{"system", "split", "doh", "custom"}
)

const dnsTag = "dns-in"

func orDefault(v, d []string) []string {
	var out []string
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return d
	}
	return out
}

// dnsHost 提取 DNS 服务器地址中的主机（IP 或域名）。
func dnsHost(server string) string {
	if strings.Contains(server, "://") {
		if u, err := url.Parse(server); err == nil {
			return u.Hostname()
		}
	}
	if h, _, err := net.SplitHostPort(server); err == nil {
		return h
	}
	return server
}

// BuildDNS 生成 dns 段以及需要插入路由最前面的规则（让国内 DNS 直连）。
// mode 为 system 时返回 nil。
func BuildDNS(d model.DNSSettings) (map[string]any, []any) {
	var servers []any
	var direct []string
	switch d.Mode {
	case "split":
		for _, s := range orDefault(d.Remote, DefaultRemoteDNS) {
			servers = append(servers, s)
		}
		for _, s := range orDefault(d.Domestic, DefaultDomesticDNS) {
			servers = append(servers, map[string]any{
				"address": s, "domains": []string{"geosite:cn"}, "expectIPs": []string{"geoip:cn"}, "skipFallback": true,
			})
			direct = append(direct, dnsHost(s))
		}
	case "doh":
		for _, s := range orDefault(d.Remote, DefaultRemoteDNS) {
			servers = append(servers, s)
		}
	case "custom":
		for _, s := range orDefault(d.Custom, nil) {
			servers = append(servers, s)
		}
	}
	if len(servers) == 0 {
		return nil, nil
	}
	dns := map[string]any{"servers": servers, "tag": dnsTag, "queryStrategy": selStr(d.QueryStrategy, "UseIP")}
	var rules []any
	var ips, domains []string
	for _, h := range direct {
		if net.ParseIP(h) != nil {
			ips = append(ips, h)
		} else if h != "" {
			domains = append(domains, "full:"+h)
		}
	}
	if len(ips) > 0 {
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{dnsTag}, "ip": ips, "outboundTag": "direct"})
	}
	if len(domains) > 0 {
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{dnsTag}, "domain": domains, "outboundTag": "direct"})
	}
	return dns, rules
}

// InboundDNSTag 是入站独立 DNS 使用的出站 tag。
func InboundDNSTag(inboundTag string) string { return "dns-" + inboundTag }

// NormalizeDNSServer 把 "8.8.8.8" / "8.8.8.8:5353" / "[2001:4860::8888]:53" / "2001:4860::8888" 规范成 IP:端口。
func NormalizeDNSServer(s string) (string, error) {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return net.JoinHostPort(ip.String(), "53"), nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || net.ParseIP(host) == nil {
		return "", fmt.Errorf("请填写 IP 地址，如 223.5.5.5 或 8.8.8.8:53")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("端口无效")
	}
	return net.JoinHostPort(host, port), nil
}
