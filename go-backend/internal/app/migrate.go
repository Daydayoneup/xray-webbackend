package app

import (
	"strings"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

const currentSchema = 6

// upgradeState 把 v1 数据（节点按序号编 tag、只保存生成后的 Xray JSON）升级到 v2：
//   - 节点/自定义代理还原出结构化的 OutboundSpec，编辑时可完整回填；
//   - 节点 tag 保持不变（规则/节点组引用不受影响），之后刷新订阅时按指纹匹配沿用；
//   - 只有一个订阅时节点直接归属该订阅，否则标记为 LegacySubID，在下次刷新时认领。
func upgradeState(st *model.PanelState) bool {
	if st.SchemaVersion >= currentSchema {
		return false
	}
	if st.SchemaVersion < 2 {
		upgradeToV2(st)
	}
	if st.SchemaVersion < 3 {
		// v3：已有订阅默认启用信息节点过滤
		for i := range st.Subscriptions {
			if st.Subscriptions[i].Exclude == "" {
				st.Subscriptions[i].Exclude = service.DefaultExclude
			}
		}
	}
	if st.SchemaVersion < 4 {
		// v4：入站单账号 → 用户列表；通知默认开启全部事件
		for i := range st.Inbounds {
			ib := &st.Inbounds[i]
			if ib.Auth != nil && len(ib.Users) == 0 && ib.Auth.User != "" {
				ib.Users = []model.InboundUser{{Name: ib.Auth.User, Secret: ib.Auth.Password}}
			}
			ib.Auth = nil
		}
		if st.Notify.Events == nil {
			st.Notify.Events = DefaultNotifyEvents()
		}
	}
	if st.SchemaVersion < 5 {
		// v5：新增用户上线/下线提醒，默认开启
		for _, id := range []string{"user_online", "user_offline"} {
			if !containsString(st.Notify.Events, id) {
				st.Notify.Events = append(st.Notify.Events, id)
			}
		}
	}
	if st.SchemaVersion < 6 {
		upgradeToV6(st)
	}
	st.SchemaVersion = currentSchema
	return true
}

// upgradeToV6 把各入站内的用户合并到全局用户表，入站改为关联用户：
//   - 同名用户合并（同一个人在 vless 与 socks 入站各有凭据），凭据原样保留，客户端无需改配置；
//   - 同名但同一类凭据不同（两个 vless 入站各有一个 alice），后者改名为 alice-入站tag；
//   - 停用的用户：若该用户在别处启用，则不关联这个入站；若处处停用，用户整体停用。
func upgradeToV6(st *model.PanelState) {
	idx := map[string]int{}
	setField := map[string]map[string]bool{} // 用户名 → 已从旧数据取得的凭据类型
	enabledSomewhere := map[string]bool{}
	type pending struct {
		name, tag string
		disabled  bool
	}
	var links []pending
	field := func(proto string) string {
		switch proto {
		case "vless":
			return "uuid"
		case "shadowsocks":
			return "ss"
		}
		return "password"
	}
	get := func(u *model.User, f string) *string {
		switch f {
		case "uuid":
			return &u.UUID
		case "ss":
			return &u.SSKey
		}
		return &u.Password
	}
	for i := range st.Inbounds {
		ib := &st.Inbounds[i]
		for _, old := range ib.Users {
			name := strings.TrimSpace(old.Name)
			if name == "" || old.Secret == "" {
				continue
			}
			f := field(ib.Protocol)
			if j, ok := idx[name]; ok && setField[name][f] && *get(&st.Users[j], f) != old.Secret {
				name = name + "-" + ib.Tag
			}
			j, ok := idx[name]
			if !ok {
				st.Users = append(st.Users, model.User{Name: name})
				j = len(st.Users) - 1
				idx[name] = j
				setField[name] = map[string]bool{}
			}
			*get(&st.Users[j], f) = old.Secret
			setField[name][f] = true
			if !old.Disabled {
				enabledSomewhere[name] = true
			}
			links = append(links, pending{name, ib.Tag, old.Disabled})
		}
		ib.Users = nil
	}
	for _, l := range links {
		u := &st.Users[idx[l.name]]
		if l.disabled && enabledSomewhere[l.name] {
			continue
		}
		if !containsString(u.Inbounds, l.tag) {
			u.Inbounds = append(u.Inbounds, l.tag)
		}
	}
	for i := range st.Users {
		u := &st.Users[i]
		u.Disabled = !enabledSomewhere[u.Name]
		service.NewUserCredentials(u)
	}
}

func upgradeToV2(st *model.PanelState) {
	single := len(st.Subscriptions) == 1
	for i := range st.Nodes {
		n := &st.Nodes[i]
		if n.Spec == nil && n.Outbound != nil {
			if spec, ok := service.SpecFromOutbound(n.Outbound); ok {
				n.Spec = spec
				n.Outbound = nil
			}
		}
		if single {
			n.SubID = st.Subscriptions[0].ID
		} else {
			n.SubID = model.LegacySubID
		}
	}
	for i := range st.Proxies {
		p := &st.Proxies[i]
		if p.Spec != nil {
			continue
		}
		var spec *model.OutboundSpec
		if p.Link != "" {
			if links, _ := service.ExtractLinks(p.Link); len(links) > 0 {
				if nodes, _ := service.ParseLinks(links); len(nodes) > 0 {
					spec = nodes[0].Spec
				}
			}
		}
		if spec == nil && p.RawOutbound != nil {
			if s, ok := service.SpecFromOutbound(p.RawOutbound); ok {
				spec = s
			}
		}
		if spec == nil && p.RawOutbound == nil && (p.Protocol == "socks" || p.Protocol == "http") {
			spec = &model.OutboundSpec{Protocol: p.Protocol, Host: p.Host, Port: p.Port}
			if p.Auth != nil {
				spec.User, spec.Pass = p.Auth.User, p.Auth.Password
			}
		}
		if spec != nil {
			p.Spec = spec
			p.RawOutbound = nil
			p.Auth = nil
		}
	}
}
