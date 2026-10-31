package service

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var PrivateCIDRs = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
}

var domainPrefix = map[string]string{
	"domain-suffix": "domain:",
	"full":          "full:",
	"keyword":       "keyword:",
	"geosite":       "geosite:",
}

// CatchAllIPs 用于 IPIfNonMatch 下的兜底规则，见 config_builder。
var CatchAllIPs = []string{"0.0.0.0/0", "::/0"}

// DomainStrategies 是允许的 routing.domainStrategy。
var DomainStrategies = []string{"IPIfNonMatch", "AsIs", "IPOnDemand"}

// DefaultDomainStrategy：域名请求未命中域名规则时再解析成 IP 匹配 geoip/ip 规则，
// 否则 "geoip:cn 直连" 这类规则对域名流量完全不生效。
const DefaultDomainStrategy = "IPIfNonMatch"

// RuleValues 把规则值按逗号/空白拆成多个（一条规则可写 "google.com, youtube.com"）。
func RuleValues(val string) []string { return splitList(val) }

func RuleToXray(rule map[string]any, balancerTags map[string]bool) map[string]any {
	r := map[string]any{"type": "field"}
	t, _ := rule["type"].(string)
	val, _ := rule["value"].(string)
	vals := RuleValues(val)

	if prefix, ok := domainPrefix[t]; ok {
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = prefix + v
		}
		r["domain"] = out
	} else if t == "ip" {
		r["ip"] = vals
	} else if t == "geoip" {
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = "geoip:" + v
		}
		r["ip"] = out
	} else if t == "port" {
		r["port"] = strings.Join(vals, ",")
	}

	tag, _ := rule["outbound"].(string)
	if balancerTags[tag] {
		r["balancerTag"] = tag
	} else {
		r["outboundTag"] = tag
	}
	return r
}

var portRe = regexp.MustCompile(`^\d{1,5}(-\d{1,5})?$`)
var codeRe = regexp.MustCompile(`^!?[A-Za-z0-9@._!-]+$`)

// ValidateRuleValue 在保存前校验规则值，避免 xray 校验时才报出难懂的错误。
func ValidateRuleValue(typ, val string) error {
	vals := RuleValues(val)
	if len(vals) == 0 {
		return fmt.Errorf("匹配值不能为空")
	}
	for _, v := range vals {
		switch typ {
		case "port":
			if !portRe.MatchString(v) {
				return fmt.Errorf("端口格式错误: %s（示例 443 或 1000-2000）", v)
			}
		case "ip":
			if net.ParseIP(v) == nil {
				if _, _, err := net.ParseCIDR(v); err != nil {
					return fmt.Errorf("IP/CIDR 格式错误: %s", v)
				}
			}
		case "set":
			if len(vals) != 1 {
				return fmt.Errorf("请选择一个分组")
			}
		case "geoip", "geosite":
			if !codeRe.MatchString(v) {
				return fmt.Errorf("预置集合名称格式错误: %s", v)
			}
		case "domain-suffix", "full", "keyword":
			if strings.Contains(v, "://") || strings.Contains(v, "/") {
				return fmt.Errorf("请只填写域名，不要带协议或路径: %s", v)
			}
		}
	}
	return nil
}

func strList(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func joinShort(vals []string) string {
	if len(vals) > 3 {
		return strings.Join(vals[:3], ", ") + fmt.Sprintf(" 等 %d 项", len(vals))
	}
	return strings.Join(vals, ", ")
}

// Describe 生成规则的人类可读描述。r 可以是内存中构建的规则，也可以是从 config.json 读回的规则。
func Describe(r map[string]any) string {
	if tag, _ := r["ruleTag"].(string); tag != "" {
		if strings.Contains(tag, "分组「") { // 分组规则：「[名称 · ]分组「X」」
			if len(strList(r["ip"])) > 0 {
				return tag + " · IP"
			}
			return tag + " · 域名"
		}
		base := make(map[string]any, len(r))
		for k, v := range r {
			if k != "ruleTag" {
				base[k] = v
			}
		}
		return tag + "（" + Describe(base) + "）"
	}
	if ips := strList(r["ip"]); len(ips) == 2 && ips[0] == CatchAllIPs[0] && ips[1] == CatchAllIPs[1] {
		return "默认出口(其余流量)"
	}
	if tags := strList(r["inboundTag"]); len(tags) > 0 {
		return "内部规则(" + strings.Join(tags, ",") + ")"
	}
	if ips := strList(r["ip"]); len(ips) > 0 {
		var geo, cidr []string
		for _, d := range ips {
			if strings.HasPrefix(d, "geoip:") {
				geo = append(geo, d[6:])
			} else {
				cidr = append(cidr, d)
			}
		}
		if len(geo) > 0 && len(cidr) == 0 {
			if len(geo) == 1 && geo[0] == "private" {
				return "内网地址"
			}
			return "地区IP " + joinShort(geo)
		}
		if len(cidr) == len(PrivateCIDRs) && cidr[0] == PrivateCIDRs[0] {
			return "内网地址"
		}
		return "IP段 " + joinShort(ips)
	}
	if domains := strList(r["domain"]); len(domains) > 0 {
		labels := []struct{ prefix, label string }{
			{"domain:", "域名后缀"}, {"full:", "完整域名"}, {"geosite:", "预置集合"}, {"keyword:", "关键字"},
		}
		d := domains[0]
		for _, l := range labels {
			if strings.HasPrefix(d, l.prefix) {
				vals := make([]string, len(domains))
				for i, x := range domains {
					vals[i] = strings.TrimPrefix(x, l.prefix)
				}
				return l.label + " " + joinShort(vals)
			}
		}
		return "关键字 " + joinShort(domains)
	}
	if port, ok := r["port"].(string); ok {
		return "端口 " + port
	}
	if port, ok := r["port"].(float64); ok {
		return fmt.Sprintf("端口 %d", int(port))
	}
	if _, ok := r["network"]; ok {
		return "默认出口(其余流量)"
	}
	return "未知规则"
}

type TemplateRule struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Outbound string `json:"outbound"` // __PROXY__ 表示由用户选择的代理出口
}

type Template struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Desc  string         `json:"desc"`
	Rules []TemplateRule `json:"rules"`
}

var Templates = []Template{
	{ID: "cn-direct", Name: "国内直连，国外走代理", Desc: "中国大陆域名与 IP 直连，其余境外站点走代理",
		Rules: []TemplateRule{
			{"geosite", "cn", "direct"},
			{"geoip", "cn", "direct"},
			{"geosite", "geolocation-!cn", "__PROXY__"},
		}},
	{ID: "block-ads", Name: "屏蔽广告", Desc: "拦截常见广告域名",
		Rules: []TemplateRule{{"geosite", "category-ads-all", "block"}}},
	{ID: "streaming", Name: "流媒体走代理", Desc: "Netflix / YouTube / Disney+ 走代理",
		Rules: []TemplateRule{{"geosite", "netflix, youtube, disney", "__PROXY__"}}},
	{ID: "ai", Name: "AI 服务走代理", Desc: "OpenAI / Anthropic / Google Gemini 等走代理",
		Rules: []TemplateRule{{"domain-suffix", "openai.com, chatgpt.com, oaistatic.com, anthropic.com, claude.ai, gemini.google.com", "__PROXY__"}}},
}
