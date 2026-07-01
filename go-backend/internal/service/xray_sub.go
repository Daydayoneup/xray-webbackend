package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NodeRaw struct {
	Name     string
	Type     string
	Host     string
	Port     int
	Latency  *int
	Outbound map[string]any
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
			skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: link[:min(60, len(link))]})
			continue
		}
		node, err := parsers[scheme](link)
		if err != nil {
			skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: fmt.Sprintf("%s (%v)", link[:min(40, len(link))], err)})
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, skipped
}

func AssignTags(nodes []NodeRaw) {
	for i := range nodes {
		nodes[i].Tag = fmt.Sprintf("node-%d", i)
	}
}

// ---- VMess ----

type vmessConfig struct {
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	AID  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	TLS  string `json:"tls"`
	Host string `json:"host"`
	Path string `json:"path"`
	SNI  string `json:"sni"`
}

func parseVMess(link string) (NodeRaw, error) {
	b64 := link[len("vmess://"):]
	raw := b64decode(b64)
	var v vmessConfig
	if err := json.Unmarshal(raw, &v); err != nil {
		return NodeRaw{}, fmt.Errorf("vmess JSON解析失败: %w", err)
	}
	port := toInt(v.Port)
	net := v.Net
	if net == "" {
		net = "tcp"
	}
	tls := strings.ToLower(v.TLS)
	sni := v.SNI
	if sni == "" {
		sni = v.Host
	}
	if sni == "" {
		sni = v.Add
	}
	path := v.Path
	if path == "" {
		path = "/"
	}
	outbound := BuildVMessOutbound(v.Add, port, v.ID, StreamOpts{
		Network:  net,
		Security: tls,
		SNI:      sni,
		Path:     path,
		Host:     v.Host,
	})
	name := v.PS
	if name == "" {
		name = v.Add
	}
	return NodeRaw{Name: name, Type: "vmess", Host: v.Add, Port: port, Outbound: outbound}, nil
}

// ---- VLess ----

func parseVLess(link string) (NodeRaw, error) {
	u, err := url.Parse(link)
	if err != nil {
		return NodeRaw{}, err
	}
	q := u.Query()
	host := u.Hostname()
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	sni := q.Get("sni")
	if sni == "" && security != "none" {
		sni = host
	}
	outbound := BuildVLessOutbound(host, port, u.User.Username(), q.Get("flow"), StreamOpts{
		Network:     net,
		Security:    security,
		SNI:         sni,
		Path:        q.Get("path"),
		Host:        q.Get("host"),
		Fingerprint: q.Get("fp"),
		PublicKey:   q.Get("pbk"),
		ShortId:     q.Get("sid"),
		SpiderX:     q.Get("spx"),
	})
	name := u.Fragment
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "vless", Host: host, Port: port, Outbound: outbound}, nil
}

// ---- Trojan ----

func parseTrojan(link string) (NodeRaw, error) {
	u, err := url.Parse(link)
	if err != nil {
		return NodeRaw{}, err
	}
	q := u.Query()
	host := u.Hostname()
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		sni = host
	}
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	outbound := BuildTrojanOutbound(host, port, u.User.Username(), StreamOpts{
		Network:  net,
		Security: "tls",
		SNI:      sni,
		Path:     q.Get("path"),
		Host:     q.Get("host"),
	})
	name := u.Fragment
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "trojan", Host: host, Port: port, Outbound: outbound}, nil
}

// ---- Shadowsocks ----

func parseSS(link string) (NodeRaw, error) {
	body := link[len("ss://"):]
	frag := ""
	if idx := strings.Index(body, "#"); idx >= 0 {
		body, frag = body[:idx], body[idx+1:]
	}
	if idx := strings.Index(body, "?"); idx >= 0 {
		body = body[:idx]
	}
	var method, password, server string
	if idx := strings.LastIndex(body, "@"); idx >= 0 {
		userinfo := body[:idx]
		server = body[idx+1:]
		dec := b64decode(userinfo)
		parts := strings.SplitN(string(dec), ":", 2)
		if len(parts) == 2 {
			method, password = parts[0], parts[1]
		} else {
			parts = strings.SplitN(userinfo, ":", 2)
			if len(parts) == 2 {
				method, password = parts[0], parts[1]
			}
		}
	} else {
		dec := b64decode(body)
		parts := strings.SplitN(string(dec), "@", 2)
		if len(parts) != 2 {
			return NodeRaw{}, fmt.Errorf("ss格式错误")
		}
		uparts := strings.SplitN(parts[0], ":", 2)
		if len(uparts) != 2 {
			return NodeRaw{}, fmt.Errorf("ss userinfo格式错误")
		}
		method, password, server = uparts[0], uparts[1], parts[1]
	}
	hostPort := strings.Split(server, ":")
	if len(hostPort) != 2 {
		return NodeRaw{}, fmt.Errorf("ss server格式错误")
	}
	host := hostPort[0]
	port := toInt(hostPort[1])
	outbound := BuildSSOutbound(host, port, method, password)
	name, _ := url.QueryUnescape(frag)
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "shadowsocks", Host: host, Port: port, Outbound: outbound}, nil
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
	addr := fmt.Sprintf("%s:%d", host, port)
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
	case string:
		n, _ := strconv.Atoi(x)
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
