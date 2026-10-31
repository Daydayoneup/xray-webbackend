package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xray-panel/internal/model"
)

type NodeRaw struct {
	Name     string
	Type     string
	Host     string
	Port     int
	Latency  *int
	Spec     *model.OutboundSpec
	Outbound map[string]any // BuildOutbound(Spec)，便于调试/测试
	Tag      string
}

type SkipInfo struct {
	Scheme string
	Detail string
}

func b64decode(s string) []byte {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	s = strings.TrimRight(s, "=")
	s += strings.Repeat("=", (4-len(s)%4)%4)
	data, _ := base64.StdEncoding.DecodeString(s)
	return data
}

func ExtractLinks(content string) ([]string, map[string]string) {
	content = strings.TrimSpace(content)
	if !strings.Contains(content, "://") {
		content = string(b64decode(content))
	}
	var links []string
	meta := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "://") {
			links = append(links, line)
		} else if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return links, meta
}

// LooksLikeClash 判断订阅内容是否为 Clash/Mihomo YAML（本面板不支持）。
func LooksLikeClash(content string) bool {
	c := strings.TrimSpace(content)
	return !strings.Contains(c, "://") &&
		(strings.Contains(c, "\nproxies:") || strings.HasPrefix(c, "proxies:") || strings.Contains(c, "proxy-groups:"))
}

type parserFunc func(link string) (NodeRaw, error)

var parsers = map[string]parserFunc{
	"vmess":  parseVMess,
	"vless":  parseVLess,
	"trojan": parseTrojan,
	"ss":     parseSS,
}

var unsupportedSchemes = map[string]bool{
	"ssr": true, "hysteria": true, "hysteria2": true, "hy2": true,
	"tuic": true, "snell": true, "wireguard": true,
}

func ParseLinks(links []string) ([]NodeRaw, []SkipInfo) {
	var nodes []NodeRaw
	var skipped []SkipInfo
	for _, link := range links {
		scheme := strings.ToLower(strings.Split(link, "://")[0])
		if unsupportedSchemes[scheme] || parsers[scheme] == nil {
			skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: "不支持的协议"})
			continue
		}
		node, err := parsers[scheme](link)
		if err == nil && node.Spec != nil {
			err = ValidateSpec(node.Spec)
		}
		if err != nil {
			skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: err.Error()})
			continue
		}
		node.Outbound = BuildOutbound(node.Spec)
		nodes = append(nodes, node)
	}
	return nodes, skipped
}

// SummarizeSkipped 汇总跳过原因，例如 "hysteria2(不支持的协议)×10, ss(不支持带插件的 ss 节点)×2"。
func SummarizeSkipped(skipped []SkipInfo) string {
	count := map[string]int{}
	for _, s := range skipped {
		count[fmt.Sprintf("%s(%s)", s.Scheme, s.Detail)]++
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return count[keys[i]] > count[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, count[k]))
	}
	return strings.Join(parts, ", ")
}

func newRaw(name, typ string, spec *model.OutboundSpec) NodeRaw {
	if name == "" {
		name = spec.Host
	}
	return NodeRaw{Name: name, Type: typ, Host: spec.Host, Port: spec.Port, Spec: spec}
}

// normNetwork 统一不同客户端分享链接中的传输名称。
func normNetwork(n string) string {
	switch strings.ToLower(n) {
	case "", "tcp", "raw":
		return ""
	case "http", "h2":
		return "h2"
	case "splithttp":
		return "xhttp"
	default:
		return strings.ToLower(n)
	}
}

// ---- VMess ----

type vmessConfig struct {
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	TLS  string `json:"tls"`
	Host string `json:"host"`
	Path string `json:"path"`
	SNI  string `json:"sni"`
	ALPN string `json:"alpn"`
	FP   string `json:"fp"`
}

func parseVMess(link string) (NodeRaw, error) {
	raw := b64decode(link[len("vmess://"):])
	var v vmessConfig
	if err := json.Unmarshal(raw, &v); err != nil {
		return NodeRaw{}, fmt.Errorf("vmess JSON解析失败")
	}
	if strings.TrimSpace(v.ID) == "" {
		return NodeRaw{}, fmt.Errorf("vmess 缺少 UUID")
	}
	spec := &model.OutboundSpec{
		Protocol: "vmess", Host: strings.TrimSpace(v.Add), Port: toInt(v.Port), UUID: strings.TrimSpace(v.ID),
		Network: normNetwork(v.Net), SNI: v.SNI, Path: v.Path, HostHeader: v.Host,
		ALPN: v.ALPN, Fingerprint: v.FP,
	}
	if scy := strings.ToLower(v.Scy); scy != "" && scy != "auto" {
		spec.Method = scy
	}
	if strings.EqualFold(v.TLS, "tls") {
		spec.Security = "tls"
	}
	if spec.Network == "grpc" && v.Type == "multi" {
		spec.Mode = "multi"
	}
	return newRaw(v.PS, "vmess", spec), nil
}

// ---- VLess / Trojan (URL 形式) ----

func parseURLLink(link, proto string) (*url.URL, *model.OutboundSpec, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, nil, fmt.Errorf("链接格式错误")
	}
	q := u.Query()
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	spec := &model.OutboundSpec{
		Protocol: proto, Host: u.Hostname(), Port: port, UUID: u.User.Username(),
		Network: normNetwork(q.Get("type")), Security: strings.ToLower(q.Get("security")),
		SNI: q.Get("sni"), Path: q.Get("path"), HostHeader: q.Get("host"), Mode: q.Get("mode"),
		ALPN: q.Get("alpn"), Fingerprint: q.Get("fp"),
		PublicKey: q.Get("pbk"), ShortID: q.Get("sid"), SpiderX: q.Get("spx"),
	}
	if spec.SNI == "" {
		spec.SNI = q.Get("peer")
	}
	if spec.Network == "grpc" {
		spec.Path = selStr(q.Get("serviceName"), spec.Path)
		if q.Get("mode") == "multi" {
			spec.Mode = "multi"
		} else {
			spec.Mode = ""
		}
	}
	if spec.Security == "none" {
		spec.Security = ""
	}
	return u, spec, nil
}

func parseVLess(link string) (NodeRaw, error) {
	u, spec, err := parseURLLink(link, "vless")
	if err != nil {
		return NodeRaw{}, err
	}
	if spec.UUID == "" {
		return NodeRaw{}, fmt.Errorf("vless 缺少 UUID")
	}
	spec.Flow = u.Query().Get("flow")
	if spec.Security == "xtls" { // 旧链接
		spec.Security = "tls"
	}
	return newRaw(u.Fragment, "vless", spec), nil
}

func parseTrojan(link string) (NodeRaw, error) {
	u, spec, err := parseURLLink(link, "trojan")
	if err != nil {
		return NodeRaw{}, err
	}
	if spec.UUID == "" {
		return NodeRaw{}, fmt.Errorf("trojan 缺少密码")
	}
	if spec.Security == "" {
		spec.Security = "tls"
	}
	return newRaw(u.Fragment, "trojan", spec), nil
}

// ---- Shadowsocks ----

func parseSS(link string) (NodeRaw, error) {
	body := link[len("ss://"):]
	frag := ""
	if idx := strings.Index(body, "#"); idx >= 0 {
		body, frag = body[:idx], body[idx+1:]
	}
	if idx := strings.Index(body, "?"); idx >= 0 {
		q, _ := url.ParseQuery(body[idx+1:])
		if q.Get("plugin") != "" {
			return NodeRaw{}, fmt.Errorf("不支持带插件的 ss 节点")
		}
		body = body[:idx]
	}
	body = strings.TrimSuffix(body, "/")
	var method, password, server string
	if idx := strings.LastIndex(body, "@"); idx >= 0 {
		// SIP002: base64(method:pass)@host:port 或 url-encoded method:pass@host:port
		userinfo := body[:idx]
		server = body[idx+1:]
		if plain, err := url.PathUnescape(userinfo); err == nil && strings.Contains(plain, ":") {
			parts := strings.SplitN(plain, ":", 2)
			method, password = parts[0], parts[1]
		} else if parts := strings.SplitN(string(b64decode(userinfo)), ":", 2); len(parts) == 2 {
			method, password = parts[0], parts[1]
		}
	} else {
		// 旧格式: base64(method:pass@host:port)
		dec := string(b64decode(body))
		idx := strings.LastIndex(dec, "@")
		if idx < 0 {
			return NodeRaw{}, fmt.Errorf("ss格式错误")
		}
		uparts := strings.SplitN(dec[:idx], ":", 2)
		if len(uparts) != 2 {
			return NodeRaw{}, fmt.Errorf("ss userinfo格式错误")
		}
		method, password, server = uparts[0], uparts[1], dec[idx+1:]
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(server))
	if err != nil {
		return NodeRaw{}, fmt.Errorf("ss server格式错误")
	}
	if method == "" || password == "" {
		return NodeRaw{}, fmt.Errorf("ss 缺少加密方式或密码")
	}
	spec := &model.OutboundSpec{
		Protocol: "shadowsocks", Host: host, Port: toInt(portStr),
		Method: strings.ToLower(method), UUID: password,
	}
	name, _ := url.PathUnescape(frag)
	return newRaw(name, "shadowsocks", spec), nil
}

// ---- TCP Ping ----

func MeasureLatency(nodes []NodeRaw) {
	sem := make(chan struct{}, 32)
	var wg sync.WaitGroup
	for i := range nodes {
		wg.Add(1)
		go func(n *NodeRaw) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			n.Latency = tcpPing(n.Host, n.Port)
		}(&nodes[i])
	}
	wg.Wait()
}

func tcpPing(host string, port int) *int {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil
	}
	conn.Close()
	ms := int(time.Since(start).Milliseconds())
	return &ms
}

// ---- Helpers ----

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func selStr(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
