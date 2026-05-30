package service

// StreamOpts holds transport/security parameters shared by all outbound builders.
type StreamOpts struct {
	Network       string // tcp, ws, grpc, h2
	Security      string // none, tls, reality
	SNI           string
	Path          string
	Host          string
	Fingerprint   string
	PublicKey     string
	ShortId       string
	SpiderX       string
	AllowInsecure bool
}

func BuildStreamSettings(opts StreamOpts) map[string]any {
	stream := map[string]any{"network": selStr(opts.Network, "tcp")}
	switch opts.Security {
	case "reality":
		stream["security"] = "reality"
		rs := map[string]any{}
		if opts.SNI != "" {
			rs["serverName"] = opts.SNI
		}
		if opts.Fingerprint != "" {
			rs["fingerprint"] = opts.Fingerprint
		}
		if opts.PublicKey != "" {
			rs["publicKey"] = opts.PublicKey
		}
		if opts.ShortId != "" {
			rs["shortId"] = opts.ShortId
		}
		if opts.SpiderX != "" {
			rs["spiderX"] = opts.SpiderX
		}
		if len(rs) > 0 {
			stream["realitySettings"] = rs
		}
	case "tls":
		stream["security"] = "tls"
		ts := map[string]any{"allowInsecure": opts.AllowInsecure}
		sni := opts.SNI
		if sni == "" {
			sni = opts.Host
		}
		if sni != "" {
			ts["serverName"] = sni
		}
		stream["tlsSettings"] = ts
	default:
		stream["security"] = "none"
	}
	net := opts.Network
	if net == "ws" {
		path := opts.Path
		if path == "" {
			path = "/"
		}
		stream["wsSettings"] = map[string]any{"path": path, "headers": map[string]any{"Host": opts.Host}}
	} else if net == "grpc" {
		svc := opts.Path
		if svc == "" {
			svc = opts.Host
		}
		stream["grpcSettings"] = map[string]any{"serviceName": svc}
	}
	return stream
}

func BuildVMessOutbound(host string, port int, uuid string, stream StreamOpts) map[string]any {
	return map[string]any{
		"protocol": "vmess",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": host, "port": port,
					"users": []any{
						map[string]any{
							"id": uuid, "alterId": 0, "security": "auto",
						},
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
