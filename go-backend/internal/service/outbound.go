package service

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"xray-panel/internal/model"
)

// StreamOpts holds transport/security parameters shared by all outbound builders.
type StreamOpts struct {
	Network     string // tcp, ws, grpc, h2, httpupgrade, xhttp
	Security    string // none, tls, reality
	SNI         string
	Path        string // ws/h2/httpupgrade/xhttp path; grpc serviceName
	Host        string // Host header
	Mode        string // xhttp mode / grpc "multi"
	ALPN        string // comma separated
	Fingerprint string
	PublicKey   string
	ShortId     string
	SpiderX     string
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func BuildStreamSettings(opts StreamOpts) map[string]any {
	network := selStr(opts.Network, "tcp")
	stream := map[string]any{"network": network}
	switch opts.Security {
	case "reality":
		stream["security"] = "reality"
		rs := map[string]any{}
		if opts.SNI != "" {
			rs["serverName"] = opts.SNI
		}
		// reality 必须带 fingerprint，缺省用 chrome
		rs["fingerprint"] = selStr(opts.Fingerprint, "chrome")
		if opts.PublicKey != "" {
			rs["publicKey"] = opts.PublicKey
		}
		if opts.ShortId != "" {
			rs["shortId"] = opts.ShortId
		}
		if opts.SpiderX != "" {
			rs["spiderX"] = opts.SpiderX
		}
		stream["realitySettings"] = rs
	case "tls":
		stream["security"] = "tls"
		// Note: allowInsecure was removed from Xray-core (migrated to
		// pinnedPeerCertSha256); we intentionally never emit it here.
		ts := map[string]any{}
		sni := opts.SNI
		if sni == "" {
			sni = opts.Host
		}
		if sni != "" {
			ts["serverName"] = sni
		}
		if opts.Fingerprint != "" {
			ts["fingerprint"] = opts.Fingerprint
		}
		if alpn := splitList(opts.ALPN); len(alpn) > 0 {
			ts["alpn"] = alpn
		}
		stream["tlsSettings"] = ts
	default:
		stream["security"] = "none"
	}

	withHost := func(m map[string]any) map[string]any {
		if opts.Host != "" {
			m["host"] = opts.Host
		}
		return m
	}
	switch network {
	case "ws":
		ws := map[string]any{"path": selStr(opts.Path, "/")}
		if opts.Host != "" {
			ws["headers"] = map[string]any{"Host": opts.Host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		g := map[string]any{"serviceName": strings.TrimPrefix(opts.Path, "/")}
		if opts.Mode == "multi" {
			g["multiMode"] = true
		}
		stream["grpcSettings"] = g
	case "h2", "http":
		stream["network"] = "h2"
		h := map[string]any{"path": selStr(opts.Path, "/")}
		if hosts := splitList(opts.Host); len(hosts) > 0 {
			h["host"] = hosts
		}
		stream["httpSettings"] = h
	case "httpupgrade":
		stream["httpupgradeSettings"] = withHost(map[string]any{"path": selStr(opts.Path, "/")})
	case "xhttp", "splithttp":
		stream["network"] = "xhttp"
		x := withHost(map[string]any{"path": selStr(opts.Path, "/")})
		if opts.Mode != "" {
			x["mode"] = opts.Mode
		}
		stream["xhttpSettings"] = x
	}
	return stream
}

func BuildVMessOutbound(host string, port int, uuid string, stream StreamOpts) map[string]any {
	return buildVMess(host, port, uuid, "auto", stream)
}

func buildVMess(host string, port int, uuid, security string, stream StreamOpts) map[string]any {
	return map[string]any{
		"protocol": "vmess",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": host, "port": port,
					"users": []any{
						map[string]any{"id": uuid, "alterId": 0, "security": selStr(security, "auto")},
					},
				},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildVLessOutbound(host string, port int, uuid string, flow string, stream StreamOpts) map[string]any {
	user := map[string]any{"id": uuid, "encryption": "none"}
	if flow != "" {
		user["flow"] = flow
	}
	return map[string]any{
		"protocol": "vless",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{"address": host, "port": port, "users": []any{user}},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildTrojanOutbound(host string, port int, password string, stream StreamOpts) map[string]any {
	return map[string]any{
		"protocol": "trojan",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{"address": host, "port": port, "password": password},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildSSOutbound(host string, port int, method string, password string) map[string]any {
	return map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{"address": host, "port": port, "method": method, "password": password},
			},
		},
	}
}

// ProxyToXray builds a socks/http outbound. Kept for backward compatibility.
func ProxyToXray(p map[string]any) map[string]any {
	server := map[string]any{"address": p["host"], "port": p["port"]}
	if auth := getAuth(p); auth != nil {
		server["users"] = []any{map[string]any{"user": auth["user"], "pass": auth["pass"]}}
	}
	return map[string]any{
		"tag":      p["tag"],
		"protocol": p["protocol"],
		"settings": map[string]any{"servers": []any{server}},
	}
}

func streamOf(s *model.OutboundSpec) StreamOpts {
	return StreamOpts{
		Network: s.Network, Security: s.Security, SNI: s.SNI, Path: s.Path,
		Host: s.HostHeader, Mode: s.Mode, ALPN: s.ALPN, Fingerprint: s.Fingerprint,
		PublicKey: s.PublicKey, ShortId: s.ShortID, SpiderX: s.SpiderX,
	}
}

// BuildOutbound 是唯一的 OutboundSpec → Xray 出站 JSON 生成入口（不含 tag）。
func BuildOutbound(s *model.OutboundSpec) map[string]any {
	switch s.Protocol {
	case "vmess":
		return buildVMess(s.Host, s.Port, s.UUID, s.Method, streamOf(s))
	case "vless":
		return BuildVLessOutbound(s.Host, s.Port, s.UUID, s.Flow, streamOf(s))
	case "trojan":
		st := streamOf(s)
		if st.Security == "" || st.Security == "none" {
			st.Security = "tls" // trojan 必须走 TLS
		}
		return BuildTrojanOutbound(s.Host, s.Port, s.UUID, st)
	case "shadowsocks":
		return BuildSSOutbound(s.Host, s.Port, s.Method, s.UUID)
	default: // socks / http
		server := map[string]any{"address": s.Host, "port": s.Port}
		if s.User != "" && s.Pass != "" {
			server["users"] = []any{map[string]any{"user": s.User, "pass": s.Pass}}
		}
		return map[string]any{
			"protocol": s.Protocol,
			"settings": map[string]any{"servers": []any{server}},
		}
	}
}

// ValidateSpec 校验手动填写的出站参数，错误信息面向用户。
func ValidateSpec(s *model.OutboundSpec) error {
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("地址不能为空")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("端口无效")
	}
	switch s.Protocol {
	case "socks", "http":
		if (s.User == "") != (s.Pass == "") {
			return fmt.Errorf("账号和密码需同时填写")
		}
	case "vmess", "vless":
		if strings.TrimSpace(s.UUID) == "" {
			return fmt.Errorf("%s 需要填写 UUID", s.Protocol)
		}
	case "trojan":
		if strings.TrimSpace(s.UUID) == "" {
			return fmt.Errorf("trojan 需要填写密码")
		}
	case "shadowsocks":
		if s.Method == "" {
			return fmt.Errorf("shadowsocks 需要选择加密方式")
		}
		if strings.TrimSpace(s.UUID) == "" {
			return fmt.Errorf("shadowsocks 需要填写密码")
		}
	default:
		return fmt.Errorf("不支持的协议: %s", s.Protocol)
	}
	if s.Security == "reality" && strings.TrimSpace(s.PublicKey) == "" {
		return fmt.Errorf("reality 需要填写 PublicKey")
	}
	return nil
}

// Fingerprint 由决定“是不是同一个节点”的字段计算，用于刷新订阅时匹配旧节点、去重。
func Fingerprint(s *model.OutboundSpec) string {
	if s == nil {
		return ""
	}
	return strings.Join([]string{
		s.Protocol, strings.ToLower(s.Host), strconv.Itoa(s.Port), s.UUID, s.Method,
		s.Network, s.Security, s.SNI, s.Path, s.HostHeader, s.Flow, s.PublicKey, s.ShortID,
	}, "|")
}

// NewNodeTag 为新节点生成稳定 tag；与 taken 冲突时追加后缀。
func NewNodeTag(subID int, fp string, taken map[string]bool) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d|%s", subID, fp)))
	base := "node-" + hex.EncodeToString(sum[:])[:8]
	tag := base
	for i := 2; taken[tag]; i++ {
		tag = fmt.Sprintf("%s-%d", base, i)
	}
	return tag
}

// SpecFromOutbound 把旧版本持久化的 Xray 出站 JSON 还原成 OutboundSpec（用于数据迁移）。
func SpecFromOutbound(ob map[string]any) (*model.OutboundSpec, bool) {
	proto, _ := ob["protocol"].(string)
	settings, _ := ob["settings"].(map[string]any)
	if settings == nil {
		return nil, false
	}
	s := &model.OutboundSpec{Protocol: proto}
	switch proto {
	case "vmess", "vless":
		v := firstInSlice(settings, "vnext")
		if v == nil {
			return nil, false
		}
		s.Host, _ = v["address"].(string)
		s.Port = toInt(v["port"])
		if users, _ := v["users"].([]any); len(users) > 0 {
			u, _ := users[0].(map[string]any)
			s.UUID, _ = u["id"].(string)
			s.Flow, _ = u["flow"].(string)
			if proto == "vmess" {
				if sec, _ := u["security"].(string); sec != "" && sec != "auto" {
					s.Method = sec
				}
			}
		}
	case "trojan", "shadowsocks", "socks", "http":
		sv := firstInSlice(settings, "servers")
		if sv == nil {
			return nil, false
		}
		s.Host, _ = sv["address"].(string)
		s.Port = toInt(sv["port"])
		s.UUID, _ = sv["password"].(string)
		s.Method, _ = sv["method"].(string)
		if users, _ := sv["users"].([]any); len(users) > 0 {
			u, _ := users[0].(map[string]any)
			s.User, _ = u["user"].(string)
			s.Pass, _ = u["pass"].(string)
		}
	default:
		return nil, false
	}
	if ss, ok := ob["streamSettings"].(map[string]any); ok {
		s.Network, _ = ss["network"].(string)
		s.Security, _ = ss["security"].(string)
		if s.Security == "none" {
			s.Security = ""
		}
		if ts, ok := ss["tlsSettings"].(map[string]any); ok {
			s.SNI, _ = ts["serverName"].(string)
			s.Fingerprint, _ = ts["fingerprint"].(string)
		}
		if rs, ok := ss["realitySettings"].(map[string]any); ok {
			s.SNI, _ = rs["serverName"].(string)
			s.Fingerprint, _ = rs["fingerprint"].(string)
			s.PublicKey, _ = rs["publicKey"].(string)
			s.ShortID, _ = rs["shortId"].(string)
			s.SpiderX, _ = rs["spiderX"].(string)
		}
		if ws, ok := ss["wsSettings"].(map[string]any); ok {
			s.Path, _ = ws["path"].(string)
			if h, ok := ws["headers"].(map[string]any); ok {
				s.HostHeader, _ = h["Host"].(string)
			}
		}
		if g, ok := ss["grpcSettings"].(map[string]any); ok {
			s.Path, _ = g["serviceName"].(string)
			// 旧版本 vmess 会把 serviceName 错误地写成 "/"
			if s.Path == "/" {
				s.Path = ""
			}
		}
	}
	if s.Network == "tcp" {
		s.Network = ""
	}
	return s, s.Host != "" && s.Port > 0
}
