package app

import (
	"testing"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

func TestUpgradeV6MergesUsers(t *testing.T) {
	ss := service.NewSSKey("2022-blake3-aes-128-gcm")
	a, _, _ := newTestApp(t, &model.PanelState{SchemaVersion: 5, Inbounds: []model.Inbound{
		{Tag: "in-0", Protocol: "socks", Port: 1080, Users: []model.InboundUser{{Name: "alice", Secret: "pw"}, {Name: "carol", Secret: "c", Disabled: true}}},
		{Tag: "in-1", Protocol: "vless", Port: 443, Users: []model.InboundUser{{Name: "alice", Secret: "11111111-1111-1111-1111-111111111111"}}},
		{Tag: "in-2", Protocol: "vless", Port: 444, Users: []model.InboundUser{{Name: "alice", Secret: "22222222-2222-2222-2222-222222222222"}, {Name: "bob", Secret: "33333333-3333-3333-3333-333333333333", Disabled: true}}},
		{Tag: "in-3", Protocol: "shadowsocks", Port: 8388, SSMethod: "2022-blake3-aes-128-gcm", Users: []model.InboundUser{{Name: "bob", Secret: ss}}},
	}})
	st := a.State()
	users := map[string]model.User{}
	for _, u := range st.Users {
		users[u.Name] = u
	}
	al := users["alice"]
	if al.Password != "pw" || al.UUID != "11111111-1111-1111-1111-111111111111" || len(al.Inbounds) != 2 {
		t.Errorf("alice merged = %+v", al)
	}
	if u := users["alice-in-2"]; u.UUID != "22222222-2222-2222-2222-222222222222" || len(u.Inbounds) != 1 || u.Inbounds[0] != "in-2" {
		t.Errorf("conflicting uuid should become a separate user: %+v", u)
	}
	// bob 在 in-2 停用、在 in-3 启用 → 只关联 in-3，用户启用；ss 密钥原样保留
	if b := users["bob"]; b.Disabled || len(b.Inbounds) != 1 || b.Inbounds[0] != "in-3" || service.SSUserKey(b.SSKey, "2022-blake3-aes-128-gcm") != ss {
		t.Errorf("bob = %+v", b)
	}
	if c := users["carol"]; !c.Disabled || len(c.Inbounds) != 1 {
		t.Errorf("carol (disabled everywhere) = %+v", c)
	}
	for _, ib := range st.Inbounds {
		if len(ib.Users) != 0 {
			t.Errorf("inbound %s still has embedded users", ib.Tag)
		}
	}
	// 生成的凭据与迁移前一致
	us := service.InboundUsers(st, &st.Inbounds[1])
	if len(us) != 1 || us[0].Secret != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("in-1 users = %+v", us)
	}
}

func TestTrafficKeysNormalized(t *testing.T) {
	ts := &trafficStore{Days: map[string]*trafficDay{"2026-01-01": {User: map[string]service.Traffic{
		"alice@in-2": {Up: 1, Down: 2}, "alice@in-3": {Up: 10, Down: 20}, "bob": {Up: 5}}}}}
	ts.normalizeUsers()
	u := ts.Days["2026-01-01"].User
	if len(u) != 2 || u["alice"].Down != 22 || u["bob"].Up != 5 {
		t.Errorf("users = %v", u)
	}
}
