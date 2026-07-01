package service

import (
	"encoding/json"
	"log/slog"
	"strings"

	"xray-panel/internal/model"
)

var observatory = map[string]any{
	"subjectSelector": []string{"node-"},
	"probeURL":        "https://www.gstatic.com/generate_204",
	"probeInterval":   "5m",
}

func BuildConfig(state *model.PanelState) map[string]any {
	balancerTags := map[string]bool{}
	for _, b := range state.Balancers {
		balancerTags[b.Tag] = true
	}

	// Outbounds missing their required credential (empty UUID/password) make the
	// latest xray-core reject the whole config; skip them so one bad node parsed
	// by older code can't take down every other node.
	skipped := map[string]bool{}

	var outbounds []any
	for _, n := range state.Nodes {
		ob := deepCopyMap(n.Outbound)
		ob["tag"] = n.Tag
		stripRemovedTLSFields(ob)
		if !outboundHasCredential(ob) {
			skipped[n.Tag] = true
			slog.Warn("跳过缺少凭据的出站节点", "tag", n.Tag, "name", n.Name)
			continue
		}
		outbounds = append(outbounds, ob)
	}
	for _, p := range state.Proxies {
		var ob map[string]any
		if p.RawOutbound != nil {
			ob = deepCopyMap(p.RawOutbound)
			stripRemovedTLSFields(ob)
		} else {
			ob = ProxyToXray(map[string]any{
				"tag": p.Tag, "protocol": p.Protocol, "host": p.Host, "port": p.Port,
			})
			if p.Auth != nil {
				ob["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["users"] = []any{
					map[string]any{"user": p.Auth.User, "pass": p.Auth.Password},
				}
			}
		}
		ob["tag"] = p.Tag
		if !outboundHasCredential(ob) {
			skipped[p.Tag] = true
			slog.Warn("跳过缺少凭据的落地代理", "tag", p.Tag, "name", p.Name)
			continue
		}
		outbounds = append(outbounds, ob)
	}
	outbounds = append(outbounds,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	)

	var rules []any
	for _, r := range state.Rules {
		if !r.Enabled || r.Value == "" || skipped[r.Outbound] {
			continue
		}
		rules = append(rules, RuleToXray(map[string]any{
			"type": r.Type, "value": r.Value, "outbound": r.Outbound,
		}, balancerTags))
	}
	rules = append(rules, map[string]any{
		"type": "field", "ip": PrivateCIDRs, "outboundTag": "direct",
	})

	defaultTag := state.DefaultOutbound
	if defaultTag == "" || skipped[defaultTag] {
		defaultTag = firstValidNodeTag(state, skipped)
	}
	tail := map[string]any{"type": "field", "network": "tcp,udp"}
	if balancerTags[defaultTag] {
		tail["balancerTag"] = defaultTag
	} else {
		tail["outboundTag"] = defaultTag
	}
	rules = append(rules, tail)

	var inbounds []any
	for _, ib := range state.Inbounds {
		auth := map[string]any(nil)
		if ib.Auth != nil {
			auth = map[string]any{"user": ib.Auth.User, "pass": ib.Auth.Password}
		}
		inbounds = append(inbounds, InboundToXray(map[string]any{
			"tag": ib.Tag, "protocol": ib.Protocol, "listen": ib.Listen,
			"port": ib.Port, "udp": ib.UDP, "auth": auth,
		}))
	}

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}

	if len(state.Balancers) > 0 {
		var bls []any
		for _, b := range state.Balancers {
			bls = append(bls, map[string]any{
				"tag": b.Tag, "selector": b.Nodes,
				"strategy": map[string]any{"type": selStr(b.Strategy, "leastPing")},
			})
		}
		cfg["routing"].(map[string]any)["balancers"] = bls
		cfg["observatory"] = observatory
	}

	return cfg
}

// outboundHasCredential reports whether ob carries the credential its protocol
// requires. A node/proxy persisted with an empty UUID/password makes the latest
// xray-core reject the entire config ("invalid UUID"), so BuildConfig skips it.
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

// firstValidNodeTag returns the tag of the first node not in skipped, or "".
func firstValidNodeTag(state *model.PanelState, skipped map[string]bool) string {
	for _, n := range state.Nodes {
		if !skipped[n.Tag] {
			return n.Tag
		}
	}
	return ""
}

// stripRemovedTLSFields removes config keys that newer Xray-core versions have
// removed and now reject outright. Nodes/proxies parsed by older code may have
// these baked into their persisted outbound map, so we scrub them at build time.
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
