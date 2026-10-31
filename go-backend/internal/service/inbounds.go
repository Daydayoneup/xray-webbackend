package service

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"xray-panel/internal/model"
)

var sniffing = map[string]any{
	"enabled":      true,
	"destOverride": []string{"http", "tls", "quic"},
}

// UserEmail 是用户在 Xray 中的 email（流量统计、在线识别的 key）。用户全局唯一，
// 同一用户在多个入站的流量合并统计。v6 之前为 name@入站tag。
func UserEmail(name string) string { return name }

func activeUsers(ib *model.Inbound) []model.InboundUser {
	var out []model.InboundUser
	for _, u := range ib.Users {
		if !u.Disabled && u.Secret != "" {
			out = append(out, u)
		}
	}
	return out
}

// BuildInbound 生成入站 JSON。
func BuildInbound(ib *model.Inbound) map[string]any {
	users := activeUsers(ib)
	settings := map[string]any{}
	var stream map[string]any
	switch ib.Protocol {
	case "socks", "http":
		var accounts []any
		for _, u := range users {
			if u.Name != "" {
				accounts = append(accounts, map[string]any{"user": u.Name, "pass": u.Secret})
			}
		}
		if ib.Protocol == "socks" {
			settings["udp"] = ib.UDP
			settings["auth"] = "noauth"
			if len(accounts) > 0 {
				settings["auth"] = "password"
			}
		}
		if len(accounts) > 0 {
			settings["accounts"] = accounts
		}
	case "vless":
		clients := []any{}
		for _, u := range users {
			clients = append(clients, map[string]any{"id": u.Secret, "flow": "xtls-rprx-vision", "email": UserEmail(u.Name)})
		}
		settings["clients"] = clients
		settings["decryption"] = "none"
		r := ib.Reality
		if r == nil {
			r = &model.RealityConf{}
		}
		stream = map[string]any{
			"network":  "tcp",
			"security": "reality",
			"realitySettings": map[string]any{
				"show": false, "dest": r.Dest, "xver": 0,
				"serverNames": r.ServerNames, "privateKey": r.PrivateKey, "shortIds": r.ShortIDs,
			},
		}
	case "shadowsocks":
		clients := []any{}
		for _, u := range users {
			clients = append(clients, map[string]any{"password": u.Secret, "email": UserEmail(u.Name)})
		}
		settings["method"] = ib.SSMethod
		settings["password"] = ib.SSKey
		settings["clients"] = clients
		settings["network"] = "tcp,udp"
	}
	out := map[string]any{
		"tag":      ib.Tag,
		"listen":   selStr(ib.Listen, "127.0.0.1"),
		"port":     ib.Port,
		"protocol": ib.Protocol,
		"settings": settings,
		"sniffing": copyMap(sniffing),
	}
	if stream != nil {
		out["streamSettings"] = stream
	}
	return out
}

// HasUserStats 表示该协议的用户可以单独统计流量。
func HasUserStats(protocol string) bool { return protocol == "vless" || protocol == "shadowsocks" }

// ShareLink 生成客户端导入用的分享链接。
func ShareLink(ib *model.Inbound, u model.InboundUser, host string) string {
	hp := net.JoinHostPort(host, strconv.Itoa(ib.Port))
	name := url.PathEscape(fmt.Sprintf("%s-%s", selStr(ib.Remark, ib.Tag), u.Name))
	switch ib.Protocol {
	case "vless":
		q := url.Values{}
		q.Set("encryption", "none")
		q.Set("flow", "xtls-rprx-vision")
		q.Set("security", "reality")
		q.Set("type", "tcp")
		q.Set("fp", "chrome")
		if r := ib.Reality; r != nil {
			if len(r.ServerNames) > 0 {
				q.Set("sni", r.ServerNames[0])
			}
			q.Set("pbk", r.PublicKey)
			if len(r.ShortIDs) > 0 {
				q.Set("sid", r.ShortIDs[0])
			}
		}
		return fmt.Sprintf("vless://%s@%s?%s#%s", u.Secret, hp, q.Encode(), name)
	case "shadowsocks":
		secret := ib.SSKey + ":" + u.Secret
		ui := base64.RawURLEncoding.EncodeToString([]byte(ib.SSMethod + ":" + secret))
		return fmt.Sprintf("ss://%s@%s#%s", ui, hp, name)
	case "socks":
		if u.Name == "" {
			return fmt.Sprintf("socks://%s#%s", hp, name)
		}
		return fmt.Sprintf("socks://%s@%s#%s", base64.RawURLEncoding.EncodeToString([]byte(u.Name+":"+u.Secret)), hp, name)
	case "http":
		if u.Name == "" {
			return fmt.Sprintf("http://%s", hp)
		}
		return fmt.Sprintf("http://%s:%s@%s", url.PathEscape(u.Name), url.PathEscape(u.Secret), hp)
	}
	return ""
}

// InboundToXray 保留给旧调用方/测试：map 形式的 socks/http 入站。
func InboundToXray(ib map[string]any) map[string]any {
	m := &model.Inbound{Protocol: fmt.Sprint(ib["protocol"]), Port: toInt(ib["port"])}
	m.Tag, _ = ib["tag"].(string)
	m.Listen, _ = ib["listen"].(string)
	m.UDP = true
	if v, ok := ib["udp"].(bool); ok {
		m.UDP = v
	}
	if a := getAuth(ib); a != nil {
		m.Users = []model.InboundUser{{Name: a["user"].(string), Secret: a["pass"].(string)}}
	}
	return BuildInbound(m)
}

func getAuth(ib map[string]any) map[string]any {
	a, _ := ib["auth"].(map[string]any)
	if a == nil {
		return nil
	}
	user, _ := a["user"].(string)
	pass, _ := a["pass"].(string)
	if user == "" || pass == "" {
		return nil
	}
	return map[string]any{"user": user, "pass": pass}
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
