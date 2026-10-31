package app

import (
	"sort"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// UserView 是用户列表的一行：用户数据 + 在线状态 + 流量。
type UserView struct {
	model.User
	Online    bool     `json:"online"`
	OnlineIPs []string `json:"online_ips"`
	TodayUp   int64    `json:"today_up"`
	TodayDown int64    `json:"today_down"`
	MonthUp   int64    `json:"month_up"` // 近 30 天
	MonthDown int64    `json:"month_down"`
}

// UserViews 返回全部用户及其在线、流量情况。
func (a *App) UserViews() []UserView {
	a.store.Lock()
	out := make([]UserView, 0, len(a.state.Users))
	for _, u := range a.state.Users {
		v := UserView{User: u, OnlineIPs: []string{}}
		if v.Inbounds == nil {
			v.Inbounds = []string{}
		}
		out = append(out, v)
	}
	a.store.Unlock()

	idx := map[string]int{}
	for i := range out {
		idx[service.UserEmail(out[i].Name)] = i
	}
	now := time.Now()
	a.online.mu.Lock()
	for _, ou := range a.online.users {
		i, ok := idx[ou.Email]
		if !ok {
			continue
		}
		for ip, ad := range ou.Addrs {
			if now.Sub(ad.LastSeen) <= onlineIdle && !containsString(out[i].OnlineIPs, ip) {
				out[i].Online = true
				out[i].OnlineIPs = append(out[i].OnlineIPs, ip)
			}
		}
	}
	a.online.mu.Unlock()
	for i := range out {
		sort.Strings(out[i].OnlineIPs)
	}

	if t := a.traffic; t != nil {
		t.mu.Lock()
		today := dayKey(now)
		for d := 0; d < 30; d++ {
			k := dayKey(now.AddDate(0, 0, -d))
			day := t.Days[k]
			if day == nil {
				continue
			}
			for email, v := range day.User {
				i, ok := idx[email]
				if !ok {
					continue
				}
				out[i].MonthUp += v.Up
				out[i].MonthDown += v.Down
				if k == today {
					out[i].TodayUp += v.Up
					out[i].TodayDown += v.Down
				}
			}
		}
		t.mu.Unlock()
	}
	return out
}
