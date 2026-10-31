package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"xray-panel/internal/model"
)

const probeURL = "https://www.gstatic.com/generate_204"

// BuildResult 是配置生成结果：Config 为 Xray JSON；Warnings 是给用户看的提示
// （例如某条规则的出口已失效而被跳过）；Unavailable 记录 tag → 不可用原因；
// Members 为每个节点组解析后的可用成员；Selected 为手动选择组实际使用的出口。
type BuildResult struct {
	Config      map[string]any
	Warnings    []string
	Unavailable map[string]string
	Members     map[string][]string
	Selected    map[string]string
	// RuleSource[i] 为生成的第 i 条路由规则对应路由页的规则序号（从 1 开始），0 表示内部规则
	RuleSource []int
}

// BuildOpts 是与部署环境相关、不属于用户数据的生成参数。
type BuildOpts struct {
	MetricsPort int // >0 时开启流量统计与 metrics 接口
}

const MetricsInboundTag = "metrics-in"

func BuildConfig(state *model.PanelState) map[string]any { return Build(state, BuildOpts{}).Config }

// GroupCandidates 返回节点组的全部成员：固定成员 + 按关键字匹配的节点（不含失效节点）。
func GroupCandidates(state *model.PanelState, b *model.Balancer) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range b.Nodes {
		add(t)
	}
	if kws := Keywords(b.Filter); len(kws) > 0 {
		for _, n := range state.Nodes {
			if n.Missing || (b.FilterSubID != nil && n.SubID != *b.FilterSubID) {
				continue
			}
			name := strings.ToLower(n.Name)
			for _, k := range kws {
				if strings.Contains(name, k) {
					add(n.Tag)
					break
				}
			}
		}
	}
	return out
}

// NodeOutbound 生成节点的出站 JSON（含 tag），nil 表示节点数据无效。
func NodeOutbound(n *model.Node) map[string]any {
	var ob map[string]any
	if n.Spec != nil {
		ob = BuildOutbound(n.Spec)
	} else if n.Outbound != nil {
		ob = deepCopyMap(n.Outbound)
		stripRemovedTLSFields(ob)
	} else {
		return nil
	}
	ob["tag"] = n.Tag
	if !outboundHasCredential(ob) {
		return nil
	}
	return ob
}

func proxyOutbound(p *model.Proxy) map[string]any {
	var ob map[string]any
	switch {
	case p.Spec != nil:
		ob = BuildOutbound(p.Spec)
	case p.RawOutbound != nil:
		ob = deepCopyMap(p.RawOutbound)
		stripRemovedTLSFields(ob)
	default:
		ob = ProxyToXray(map[string]any{"tag": p.Tag, "protocol": p.Protocol, "host": p.Host, "port": p.Port})
		if p.Auth != nil {
			ob["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["users"] = []any{
				map[string]any{"user": p.Auth.User, "pass": p.Auth.Password},
			}
		}
	}
	ob["tag"] = p.Tag
	if !outboundHasCredential(ob) {
		return nil
	}
	return ob
}

func setDialerProxy(ob map[string]any, via string) {
	ss, _ := ob["streamSettings"].(map[string]any)
	if ss == nil {
		ss = map[string]any{}
		ob["streamSettings"] = ss
	}
	so, _ := ss["sockopt"].(map[string]any)
	if so == nil {
		so = map[string]any{}
		ss["sockopt"] = so
	}
	so["dialerProxy"] = via
}

func Build(state *model.PanelState, opts BuildOpts) BuildResult {
	res := BuildResult{Unavailable: map[string]string{}, Members: map[string][]string{}, Selected: map[string]string{}}
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	labels := map[string]string{"direct": "直连", "block": "阻断"}
	available := map[string]bool{"direct": true, "block": true}
	var outbounds []any

	// 1) 订阅节点
	for i := range state.Nodes {
		n := &state.Nodes[i]
		labels[n.Tag] = n.Name
		switch {
		case n.Missing:
			res.Unavailable[n.Tag] = "订阅中已不存在"
			continue
		case n.Disabled:
			res.Unavailable[n.Tag] = "已禁用"
			continue
		}
		ob := NodeOutbound(n)
		if ob == nil {
			res.Unavailable[n.Tag] = "缺少 UUID/密码"
			continue
		}
		available[n.Tag] = true
		outbounds = append(outbounds, ob)
	}

	// 2) 自定义代理：链式代理需要前置出口可用，反复解析直到稳定（可处理多级链与环）
	proxyObs := map[string]map[string]any{}
	for i := range state.Proxies {
		p := &state.Proxies[i]
		labels[p.Tag] = p.Name
		ob := proxyOutbound(p)
		if ob == nil {
			res.Unavailable[p.Tag] = "缺少 UUID/密码"
			continue
		}
		proxyObs[p.Tag] = ob
	}
	for changed := true; changed; {
		changed = false
		for i := range state.Proxies {
			p := &state.Proxies[i]
			if available[p.Tag] || proxyObs[p.Tag] == nil {
				continue
			}
			if p.Via == "" || available[p.Via] {
				available[p.Tag] = true
				changed = true
			}
		}
	}
	for i := range state.Proxies {
		p := &state.Proxies[i]
		ob := proxyObs[p.Tag]
		if ob == nil {
			continue
		}
		if !available[p.Tag] {
			reason := fmt.Sprintf("前置出口 %s 不可用", p.Via)
			res.Unavailable[p.Tag] = reason
			warn("自定义代理「%s」%s，已跳过", p.Name, reason)
			continue
		}
		if p.Via != "" {
			setDialerProxy(ob, p.Via)
		}
		outbounds = append(outbounds, ob)
	}
	outbounds = append(outbounds,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	)

	// 3) 节点组：自动策略生成 Xray balancer；手动选择组直接映射到所选出口
	balancerTags := map[string]bool{}
	alias := map[string]string{} // 手动选择组 tag → 实际出口
	var bls []any
	probeSet := map[string]bool{}
	var probeTags []string
	for i := range state.Balancers {
		b := &state.Balancers[i]
		labels[b.Tag] = b.Name
		var members []string
		cands := GroupCandidates(state, b)
		for _, m := range cands {
			if available[m] {
				members = append(members, m)
			}
		}
		res.Members[b.Tag] = members
		if len(members) == 0 {
			reason := "没有可用成员"
			if len(cands) == 0 && b.Filter != "" {
				reason = "关键字没有匹配到任何节点"
			}
			res.Unavailable[b.Tag] = reason
			warn("节点组「%s」%s，已跳过", b.Name, reason)
			continue
		}
		if n := len(cands) - len(members); n > 0 {
			warn("节点组「%s」有 %d 个成员不可用", b.Name, n)
		}
		for _, m := range members {
			if !probeSet[m] {
				probeSet[m] = true
				probeTags = append(probeTags, m)
			}
		}
		available[b.Tag] = true
		if b.Strategy == "select" {
			sel := b.Selected
			if !containsStr(members, sel) {
				if sel != "" {
					warn("节点组「%s」选中的「%s」不可用，临时改用「%s」", b.Name, labels[sel], labels[members[0]])
				}
				sel = members[0]
			}
			alias[b.Tag] = sel
			res.Selected[b.Tag] = sel
			continue
		}
		balancerTags[b.Tag] = true
		bls = append(bls, map[string]any{
			"tag": b.Tag, "selector": members,
			"strategy": map[string]any{"type": selStr(b.Strategy, "leastPing")},
		})
	}
	label := func(tag string) string {
		if l := labels[tag]; l != "" {
			return l
		}
		return tag
	}
	target := func(tag string) string {
		if t, ok := alias[tag]; ok {
			return t
		}
		return tag
	}

	// 4) 分流规则
	var rules []any
	var src []int
	if opts.MetricsPort > 0 {
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{MetricsInboundTag}, "outboundTag": "metrics"})
		src = append(src, 0)
	}
	dns, dnsRules := BuildDNS(state.DNS)
	rules = append(rules, dnsRules...)
	for range dnsRules {
		src = append(src, 0)
	}
	// 入站独立 DNS：该入站的 53 端口流量由 freedom 改发到指定服务器（可经某个出口）
	for i := range state.Inbounds {
		ib := &state.Inbounds[i]
		if ib.DNS == nil || ib.DNS.Server == "" {
			continue
		}
		addr, err := NormalizeDNSServer(ib.DNS.Server)
		if err != nil {
			warn("入站 %s 的 DNS 服务器无效（%s），已忽略", selStr(ib.Remark, ib.Tag), err)
			continue
		}
		ob := map[string]any{"tag": InboundDNSTag(ib.Tag), "protocol": "freedom", "settings": map[string]any{"redirect": addr}}
		if via := ib.DNS.Via; via != "" && via != "direct" {
			switch {
			case !available[via]:
				warn("入站 %s 的 DNS 出口「%s」不可用，改为直连", selStr(ib.Remark, ib.Tag), label(via))
			case balancerTags[target(via)]:
				warn("入站 %s 的 DNS 出口不能是自动选择的节点组（Xray 限制），改为直连", selStr(ib.Remark, ib.Tag))
			default:
				ob["streamSettings"] = map[string]any{"sockopt": map[string]any{"dialerProxy": target(via)}}
			}
		}
		outbounds = append(outbounds, ob)
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{ib.Tag}, "port": "53", "outboundTag": InboundDNSTag(ib.Tag)})
		src = append(src, 0)
	}
	for i, r := range state.Rules {
		if !r.Enabled || strings.TrimSpace(r.Value) == "" {
			continue
		}
		if !available[r.Outbound] {
			warn("规则 #%d（%s）的出口「%s」不可用，已跳过", i+1, selStr(r.Name, r.Value), label(r.Outbound))
			continue
		}
		if r.Type == "set" {
			rs := findRuleSet(state, r.Value)
			if rs == nil {
				warn("规则 #%d 引用的分组已不存在，已跳过", i+1)
				continue
			}
			if len(rs.Domains) == 0 && len(rs.IPs) == 0 {
				warn("规则 #%d 引用的分组「%s」是空的，已跳过", i+1, rs.Name)
				continue
			}
			// 域名与 IP 必须分成两条规则：同一条规则里的 domain 和 ip 是“且”的关系。
			// 两条的 ruleTag 必须不同（新版 Xray 拒绝重复的 ruleTag），分别以「 · 域名」「 · IP」结尾
			for _, part := range []struct {
				key, label string
				vals       []string
			}{{"domain", "域名", SetXrayDomains(rs)}, {"ip", "IP", rs.IPs}} {
				if len(part.vals) == 0 {
					continue
				}
				tag := RuleSetTag(rs)
				if r.Name != "" {
					tag = r.Name + " · " + tag
				}
				tag += " · " + part.label
				xr := map[string]any{"type": "field", "ruleTag": tag, part.key: part.vals}
				if t := target(r.Outbound); balancerTags[t] {
					xr["balancerTag"] = t
				} else {
					xr["outboundTag"] = t
				}
				rules = append(rules, xr)
				src = append(src, i+1)
			}
			continue
		}
		xr := RuleToXray(map[string]any{
			"type": r.Type, "value": r.Value, "outbound": target(r.Outbound),
		}, balancerTags)
		if r.Name != "" {
			xr["ruleTag"] = r.Name // Xray 只用于日志；面板的生效路由、规则测试用它显示名称
		}
		rules = append(rules, xr)
		src = append(src, i+1)
	}
	rules = append(rules, map[string]any{
		"type": "field", "ip": PrivateCIDRs, "outboundTag": "direct",
	})
	src = append(src, 0)

	// 5) 默认出口
	defaultTag := state.DefaultOutbound
	if defaultTag == "" || !available[defaultTag] {
		fallback := "direct"
		for i := range state.Nodes {
			if available[state.Nodes[i].Tag] {
				fallback = state.Nodes[i].Tag
				break
			}
		}
		if defaultTag != "" {
			warn("默认出口「%s」不可用，临时改用「%s」", label(defaultTag), label(fallback))
		}
		defaultTag = fallback
	}
	defaultTag = target(defaultTag)
	// 兜底规则：IPIfNonMatch 下 Xray 只有在“第一轮没有任何规则命中”时才会把域名解析成 IP
	// 再匹配一轮。若兜底规则用 network 匹配，第一轮必然命中，geoip 规则对域名流量永远不生效。
	// 因此 IPIfNonMatch 时兜底规则改为匹配全部 IP：第一轮（只有域名）不命中，第二轮兜住其余流量。
	tail := map[string]any{"type": "field", "network": "tcp,udp"}
	if selStr(state.DomainStrategy, DefaultDomainStrategy) == "IPIfNonMatch" {
		tail = map[string]any{"type": "field", "ip": CatchAllIPs}
	}
	if balancerTags[defaultTag] {
		tail["balancerTag"] = defaultTag
	} else {
		tail["outboundTag"] = defaultTag
	}
	rules = append(rules, tail)
	res.RuleSource = append(src, 0)
	uniqueRuleTags(rules, res.RuleSource)

	// 6) 入站
	inbounds := []any{}
	for i := range state.Inbounds {
		ib := &state.Inbounds[i]
		ib2 := *ib
		ib2.Users = InboundUsers(state, ib)
		if ib.Auth != nil && len(ib2.Users) == 0 { // 旧数据
			ib2.Users = []model.InboundUser{{Name: ib.Auth.User, Secret: ib.Auth.Password}}
		}
		ib = &ib2
		if (ib.Protocol == "vless" || ib.Protocol == "shadowsocks") && len(activeUsers(ib)) == 0 {
			warn("入站 %s（%s:%d）没有启用的用户，已跳过", selStr(ib.Remark, ib.Tag), ib.Protocol, ib.Port)
			continue
		}
		inbounds = append(inbounds, BuildInbound(ib))
	}
	if len(inbounds) == 0 {
		warn("没有任何入站，Xray 不会监听任何端口")
	}

	routing := map[string]any{
		"domainStrategy": selStr(state.DomainStrategy, DefaultDomainStrategy),
		"rules":          rules,
	}
	// 没有规则命中时（如 DNS 解析失败）Xray 使用第一个出站：把默认出口放到最前
	if !balancerTags[defaultTag] {
		for i, ob := range outbounds {
			if ob.(map[string]any)["tag"] == defaultTag && i > 0 {
				outbounds = append([]any{ob}, append(outbounds[:i:i], outbounds[i+1:]...)...)
				break
			}
		}
	}
	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   routing,
	}
	if dns != nil {
		cfg["dns"] = dns
	}
	if len(bls) > 0 {
		routing["balancers"] = bls
	}
	if len(probeTags) > 0 {
		cfg["observatory"] = map[string]any{
			"subjectSelector":   probeTags,
			"probeURL":          probeURL,
			"probeInterval":     "1m",
			"enableConcurrency": true,
		}
	}
	if opts.MetricsPort > 0 {
		cfg["stats"] = map[string]any{}
		cfg["metrics"] = map[string]any{"tag": "metrics"}
		cfg["policy"] = map[string]any{
			"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}},
			"system": map[string]any{
				"statsInboundUplink": true, "statsInboundDownlink": true,
				"statsOutboundUplink": true, "statsOutboundDownlink": true,
			},
		}
		cfg["inbounds"] = append(inbounds, map[string]any{
			"tag": MetricsInboundTag, "listen": "127.0.0.1", "port": opts.MetricsPort,
			"protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"},
		})
	}
	res.Config = cfg
	return res
}

func findRuleSet(state *model.PanelState, tag string) *model.RuleSet {
	for i := range state.RuleSets {
		if state.RuleSets[i].Tag == tag {
			return &state.RuleSets[i]
		}
	}
	return nil
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// outboundHasCredential reports whether ob carries the credential its protocol
// requires. A node/proxy persisted with an empty UUID/password makes the latest
// xray-core reject the entire config ("invalid UUID"), so Build skips it.
func outboundHasCredential(ob map[string]any) bool {
	settings, _ := ob["settings"].(map[string]any)
	if settings == nil {
		return true
	}
	switch ob["protocol"] {
	case "vmess", "vless":
		v := firstInSlice(settings, "vnext")
		if v == nil {
			return false
		}
		users, _ := v["users"].([]any)
		if len(users) == 0 {
			return false
		}
		u, _ := users[0].(map[string]any)
		id, _ := u["id"].(string)
		return strings.TrimSpace(id) != ""
	case "trojan", "shadowsocks":
		s := firstInSlice(settings, "servers")
		if s == nil {
			return false
		}
		if ob["protocol"] == "shadowsocks" {
			// 不支持的加密方式（如 none）会让 Xray 整个配置加载失败，按无效节点处理
			if m, _ := s["method"].(string); !SupportedSSMethod(m) {
				return false
			}
		}
		pw, _ := s["password"].(string)
		return strings.TrimSpace(pw) != ""
	default:
		return true // socks/http/freedom/blackhole carry no such credential
	}
}

// firstInSlice returns the first element of settings[key] as a map, or nil.
func firstInSlice(settings map[string]any, key string) map[string]any {
	arr, _ := settings[key].([]any)
	if len(arr) == 0 {
		return nil
	}
	m, _ := arr[0].(map[string]any)
	return m
}

// stripRemovedTLSFields removes config keys that newer Xray-core versions have
// removed and now reject outright. Legacy outbound maps may have these baked in.
func stripRemovedTLSFields(ob map[string]any) {
	ss, ok := ob["streamSettings"].(map[string]any)
	if !ok {
		return
	}
	if ts, ok := ss["tlsSettings"].(map[string]any); ok {
		delete(ts, "allowInsecure")
	}
}

func deepCopyMap(src map[string]any) map[string]any {
	data, _ := json.Marshal(src)
	var dst map[string]any
	json.Unmarshal(data, &dst)
	return dst
}

// uniqueRuleTags 保证 ruleTag 不重复（新版 Xray 遇到重复的 ruleTag 会拒绝启动）：
// 旧数据中同名的规则在名称后加上路由页的规则序号区分。
func uniqueRuleTags(rules []any, src []int) {
	seen := map[string]bool{}
	for i, x := range rules {
		r, _ := x.(map[string]any)
		tag, _ := r["ruleTag"].(string)
		if tag == "" {
			continue
		}
		if seen[tag] {
			n := 0
			if i < len(src) {
				n = src[i]
			}
			base := tag
			for k := 1; seen[tag]; k++ {
				tag = fmt.Sprintf("%s #%d", base, n)
				if k > 1 {
					tag = fmt.Sprintf("%s #%d-%d", base, n, k)
				}
			}
			r["ruleTag"] = tag
		}
		seen[tag] = true
	}
}
