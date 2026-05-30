package service

var PrivateCIDRs = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
}

var domainPrefix = map[string]string{
	"domain-suffix": "domain:",
	"full":          "full:",
	"keyword":       "",
	"geosite":       "geosite:",
}

func RuleToXray(rule map[string]any, balancerTags map[string]bool) map[string]any {
	r := map[string]any{"type": "field"}
	t, _ := rule["type"].(string)
	val, _ := rule["value"].(string)

	if prefix, ok := domainPrefix[t]; ok {
		r["domain"] = []string{prefix + val}
	} else if t == "ip" {
		r["ip"] = []string{val}
	} else if t == "geoip" {
		r["ip"] = []string{"geoip:" + val}
	} else if t == "port" {
		r["port"] = val
	}

	tag, _ := rule["outbound"].(string)
	if balancerTags[tag] {
		r["balancerTag"] = tag
	} else {
		r["outboundTag"] = tag
	}
	return r
}

func Describe(r map[string]any) string {
	if ip, ok := r["ip"].([]string); ok && len(ip) > 0 {
		d := ip[0]
		if len(d) > 6 && d[:6] == "geoip:" {
			return "地区IP " + d
		}
		return "IP段 " + d
	}
	if domain, ok := r["domain"].([]string); ok && len(domain) > 0 {
		d := domain[0]
		if len(d) > 7 && d[:7] == "domain:" {
			return "域名后缀 " + d[7:]
		}
		if len(d) > 5 && d[:5] == "full:" {
			return "完整域名 " + d[5:]
		}
		if len(d) > 8 && d[:8] == "geosite:" {
			return "预置集合 " + d
		}
		return "关键字 " + d
	}
	if port, ok := r["port"].(string); ok {
		return "端口 " + port
	}
	if _, ok := r["network"]; ok {
		return "默认出口(其余流量)"
	}
	return "未知规则"
}

var Templates = map[string][]map[string]string{
	"cn-direct": {
		{"type": "geoip", "value": "cn", "outbound": "direct"},
		{"type": "geosite", "value": "cn", "outbound": "direct"},
		{"type": "geosite", "value": "geolocation-!cn", "outbound": "__PROXY__"},
	},
	"block-ads": {
		{"type": "geosite", "value": "category-ads-all", "outbound": "block"},
	},
	"streaming": {
		{"type": "geosite", "value": "netflix", "outbound": "__PROXY__"},
		{"type": "geosite", "value": "youtube", "outbound": "__PROXY__"},
		{"type": "geosite", "value": "disney", "outbound": "__PROXY__"},
	},
}
