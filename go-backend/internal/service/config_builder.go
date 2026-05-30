package service

import (
	"encoding/json"

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

	var outbounds []any
	for _, n := range state.Nodes {
		ob := deepCopyMap(n.Outbound)
		ob["tag"] = n.Tag
		outbounds = append(outbounds, ob)
	}
	for _, p := range state.Proxies {
		var ob map[string]any
		if p.RawOutbound != nil {
			ob = deepCopyMap(p.RawOutbound)
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
		outbounds = append(outbounds, ob)
	}
	outbounds = append(outbounds,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	)

	var rules []any
	for _, r := range state.Rules {
		if !r.Enabled || r.Value == "" {
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
	if defaultTag == "" && len(state.Nodes) > 0 {
		defaultTag = state.Nodes[0].Tag
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

func deepCopyMap(src map[string]any) map[string]any {
	data, _ := json.Marshal(src)
	var dst map[string]any
	json.Unmarshal(data, &dst)
	return dst
}
