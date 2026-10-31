package app

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// onlineIdle 内既没有新连接、也没有流量即视为掉线。Xray 不报告连接断开，只能按空闲时间判断。
const onlineIdle = 3 * time.Minute

const maxOnlineEvents = 500

type onlineAddr struct {
	IP        string
	Networks  map[string]bool
	FirstSeen time.Time
	LastSeen  time.Time
	Conns     int
	LastDest  string
	Outbound  string
}

// onlineUser 是一个在线用户：同一入站下按 email 区分；入站未开启鉴权时没有 email，按来源 IP 区分。
type onlineUser struct {
	Inbound   string
	Email     string
	Addrs     map[string]*onlineAddr
	FirstSeen time.Time
	LastSeen  time.Time
	Conns     int
	Up, Down  int64
}

func (u *onlineUser) latest() *onlineAddr {
	var best *onlineAddr
	for _, a := range u.Addrs {
		if best == nil || a.LastSeen.After(best.LastSeen) {
			best = a
		}
	}
	return best
}

// OnlineEvent 是一次上线/新地址/下线记录。
type OnlineEvent struct {
	Time     int64  `json:"time"`
	Kind     string `json:"kind"` // online | new_ip | offline
	Inbound  string `json:"inbound"`
	Email    string `json:"email,omitempty"`
	IP       string `json:"ip"`
	Network  string `json:"network,omitempty"`
	Duration int64  `json:"duration,omitempty"` // 下线时：在线时长（秒）
	Up       int64  `json:"up,omitempty"`       // 下线时：本次在线流量
	Down     int64  `json:"down,omitempty"`
	// 以下由 App 填充
	InboundLabel string `json:"inbound_label,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	User         string `json:"user,omitempty"`
}

type onlineTracker struct {
	mu     sync.Mutex
	users  map[string]*onlineUser
	events []OnlineEvent
	emit   func([]OnlineEvent) // 在锁外调用
}

func newOnlineTracker() *onlineTracker { return &onlineTracker{users: map[string]*onlineUser{}} }

func onlineKey(inbound, email, ip string) string {
	if email != "" {
		return inbound + "|" + email
	}
	return inbound + "|ip:" + ip
}

func (t *onlineTracker) record(evs []OnlineEvent) {
	t.events = append(t.events, evs...)
	if over := len(t.events) - maxOnlineEvents; over > 0 {
		t.events = append([]OnlineEvent(nil), t.events[over:]...)
	}
}

// observe 处理一条访问日志。
func (t *onlineTracker) observe(ev service.AccessEvent, now time.Time) {
	if ev.Inbound == "" || ev.Inbound == service.MetricsInboundTag || ev.IP == "" {
		return
	}
	var out []OnlineEvent
	t.mu.Lock()
	key := onlineKey(ev.Inbound, ev.Email, ev.IP)
	u := t.users[key]
	if u == nil {
		u = &onlineUser{Inbound: ev.Inbound, Email: ev.Email, Addrs: map[string]*onlineAddr{}, FirstSeen: now}
		t.users[key] = u
		out = append(out, OnlineEvent{Kind: "online"})
	}
	a := u.Addrs[ev.IP]
	if a == nil {
		a = &onlineAddr{IP: ev.IP, Networks: map[string]bool{}, FirstSeen: now}
		u.Addrs[ev.IP] = a
		if len(out) == 0 {
			out = append(out, OnlineEvent{Kind: "new_ip"})
		}
	}
	a.Networks[ev.Network] = true
	a.LastSeen, a.Conns, a.LastDest, a.Outbound = now, a.Conns+1, ev.Dest, ev.Outbound
	u.LastSeen, u.Conns = now, u.Conns+1
	for i := range out {
		out[i].Time, out[i].Inbound, out[i].Email, out[i].IP, out[i].Network = now.Unix(), ev.Inbound, ev.Email, ev.IP, ev.Network
	}
	t.record(out)
	emit := t.emit
	t.mu.Unlock()
	if len(out) > 0 && emit != nil {
		emit(out)
	}
}

// addTraffic 把采样增量计入在线用户，并刷新活跃时间（长连接不会产生新的访问日志）。
func (t *onlineTracker) addTraffic(d trafficDelta, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	touch := func(u *onlineUser, v service.Traffic) {
		if v.Up == 0 && v.Down == 0 {
			return
		}
		u.Up += v.Up
		u.Down += v.Down
		u.LastSeen = now
		if a := u.latest(); a != nil {
			a.LastSeen = now
		}
	}
	byInbound := map[string][]*onlineUser{}
	for _, u := range t.users {
		if u.Email != "" {
			touch(u, d.User[u.Email])
		}
		byInbound[u.Inbound] = append(byInbound[u.Inbound], u)
	}
	// 未鉴权的入站只有入站级计数：仅当该入站只有一个在线来源时才能归属
	for tag, us := range byInbound {
		if len(us) == 1 && us[0].Email == "" {
			touch(us[0], d.Inbound[tag])
		}
	}
}

// sweep 移除空闲超时的地址/用户，返回下线事件。
func (t *onlineTracker) sweep(now time.Time) {
	var out []OnlineEvent
	t.mu.Lock()
	for key, u := range t.users {
		last := u.latest()
		for ip, a := range u.Addrs {
			if now.Sub(a.LastSeen) > onlineIdle {
				delete(u.Addrs, ip)
			}
		}
		if len(u.Addrs) > 0 {
			continue
		}
		delete(t.users, key)
		ev := OnlineEvent{Time: now.Unix(), Kind: "offline", Inbound: u.Inbound, Email: u.Email,
			Duration: int64(u.LastSeen.Sub(u.FirstSeen).Seconds()), Up: u.Up, Down: u.Down}
		if last != nil {
			ev.IP = last.IP
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Inbound+out[i].Email+out[i].IP < out[j].Inbound+out[j].Email+out[j].IP
	})
	t.record(out)
	emit := t.emit
	t.mu.Unlock()
	if len(out) > 0 && emit != nil {
		emit(out)
	}
}

// ---------- 对外视图 ----------

type OnlineAddr struct {
	IP        string   `json:"ip"`
	Networks  []string `json:"networks"`
	FirstSeen int64    `json:"first_seen"`
	LastSeen  int64    `json:"last_seen"`
	Conns     int      `json:"conns"`
	LastDest  string   `json:"last_dest"`
	Outbound  string   `json:"outbound"`
}

type OnlineUser struct {
	Inbound      string       `json:"inbound"`
	InboundLabel string       `json:"inbound_label"`
	Protocol     string       `json:"protocol"`
	User         string       `json:"user"`
	Email        string       `json:"email,omitempty"`
	Addrs        []OnlineAddr `json:"addrs"`
	FirstSeen    int64        `json:"first_seen"`
	LastSeen     int64        `json:"last_seen"`
	Conns        int          `json:"conns"`
	Up           int64        `json:"up"`
	Down         int64        `json:"down"`
	TodayUp      int64        `json:"today_up"`
	TodayDown    int64        `json:"today_down"`
	HasTraffic   bool         `json:"has_traffic"` // 能否单独统计该用户流量
}

type OnlineReport struct {
	Running        bool          `json:"running"`
	MetricsEnabled bool          `json:"metrics_enabled"`
	IdleSeconds    int           `json:"idle_seconds"`
	Summary        OnlineSummary `json:"summary"`
	Total          int           `json:"total"` // 筛选后的在线用户数（Users 为其中一页）
	Users          []OnlineUser  `json:"users"`
	EventsTotal    int           `json:"events_total"`
	Events         []OnlineEvent `json:"events"` // 新的在前
}

// OnlineSummary 是不受筛选影响的整体统计。
type OnlineSummary struct {
	Users     int             `json:"users"`
	Addrs     int             `json:"addrs"` // 不同的来源 IP 数
	Anonymous int             `json:"anonymous"`
	ByInbound []InboundOnline `json:"by_inbound"`
}

type InboundOnline struct {
	Inbound string `json:"inbound"`
	Label   string `json:"label"`
	Users   int    `json:"users"`
}

// OnlineQuery 是在线列表的筛选、排序与分页参数。
type OnlineQuery struct {
	Q           string // 匹配用户名、IP、入站名
	Inbound     string
	Sort        string // traffic | recent | duration | conns | name（默认按入站、用户名）
	Offset      int
	Limit       int    // <=0 表示全部
	EventKind   string // online（含新地址）| offline
	EventOffset int
	EventLimit  int
}

var protocolNames = map[string]string{
	"socks": "SOCKS5", "http": "HTTP", "vless": "VLESS + Reality", "shadowsocks": "Shadowsocks 2022",
}

// describeInbound 返回入站显示名、协议名。调用方需持有 Store 锁。
func (a *App) describeInbound(tag string) (*model.Inbound, string, string) {
	for i := range a.state.Inbounds {
		ib := &a.state.Inbounds[i]
		if ib.Tag == tag {
			return ib, selOrStr(ib.Remark, protocolNames[ib.Protocol]) + " :" + itoa(ib.Port), selOrStr(protocolNames[ib.Protocol], ib.Protocol)
		}
	}
	return nil, tag, ""
}

// userDisplay 把 email 还原成用户名。vless/ss 的 email 为 name@tag，socks/http 为用户名本身。
func userDisplay(ib *model.Inbound, email string) string {
	if email == "" {
		return ""
	}
	if ib != nil {
		if name, ok := strings.CutSuffix(email, "@"+ib.Tag); ok {
			return name
		}
	}
	return email
}

// fillEvent 补全事件的显示字段。调用方需持有 Store 锁。
func (a *App) fillEvent(ev *OnlineEvent) {
	ib, label, proto := a.describeInbound(ev.Inbound)
	ev.InboundLabel, ev.Protocol, ev.User = label, proto, userDisplay(ib, ev.Email)
}

// Online 按查询条件返回在线用户（一页）与上下线记录。
func (a *App) Online(q OnlineQuery) OnlineReport {
	rep := a.onlineAll()
	// 整体统计
	ips := map[string]bool{}
	by := map[string]*InboundOnline{}
	var order []string
	for _, u := range rep.Users {
		rep.Summary.Users++
		if u.Email == "" {
			rep.Summary.Anonymous++
		}
		for _, ad := range u.Addrs {
			ips[ad.IP] = true
		}
		if by[u.Inbound] == nil {
			by[u.Inbound] = &InboundOnline{Inbound: u.Inbound, Label: u.InboundLabel}
			order = append(order, u.Inbound)
		}
		by[u.Inbound].Users++
	}
	rep.Summary.Addrs = len(ips)
	rep.Summary.ByInbound = []InboundOnline{}
	for _, t := range order {
		rep.Summary.ByInbound = append(rep.Summary.ByInbound, *by[t])
	}
	sort.SliceStable(rep.Summary.ByInbound, func(i, j int) bool { return rep.Summary.ByInbound[i].Users > rep.Summary.ByInbound[j].Users })

	// 筛选
	kw := strings.ToLower(strings.TrimSpace(q.Q))
	match := func(u *OnlineUser) bool {
		if q.Inbound != "" && u.Inbound != q.Inbound {
			return false
		}
		if kw == "" {
			return true
		}
		if strings.Contains(strings.ToLower(u.User), kw) || strings.Contains(strings.ToLower(u.InboundLabel), kw) {
			return true
		}
		for _, ad := range u.Addrs {
			if strings.Contains(ad.IP, kw) {
				return true
			}
		}
		return false
	}
	users := []OnlineUser{}
	for i := range rep.Users {
		if match(&rep.Users[i]) {
			users = append(users, rep.Users[i])
		}
	}
	less := map[string]func(x, y *OnlineUser) bool{
		"traffic":  func(x, y *OnlineUser) bool { return x.Up+x.Down > y.Up+y.Down },
		"recent":   func(x, y *OnlineUser) bool { return x.LastSeen > y.LastSeen },
		"duration": func(x, y *OnlineUser) bool { return x.FirstSeen < y.FirstSeen },
		"conns":    func(x, y *OnlineUser) bool { return x.Conns > y.Conns },
		"name":     func(x, y *OnlineUser) bool { return x.User < y.User },
	}[q.Sort]
	if less != nil {
		sort.SliceStable(users, func(i, j int) bool { return less(&users[i], &users[j]) })
	}
	rep.Total = len(users)
	if q.Offset > 0 {
		users = users[min(q.Offset, len(users)):]
	}
	if q.Limit > 0 && len(users) > q.Limit {
		users = users[:q.Limit]
	}
	rep.Users = users

	// 上下线记录
	evs := []OnlineEvent{}
	for _, e := range rep.Events {
		if q.EventKind == "online" && e.Kind == "offline" || q.EventKind == "offline" && e.Kind != "offline" {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(e.User), kw) && !strings.Contains(e.IP, kw) {
			continue
		}
		if q.Inbound != "" && e.Inbound != q.Inbound {
			continue
		}
		evs = append(evs, e)
	}
	rep.EventsTotal = len(evs)
	limit := q.EventLimit
	if limit <= 0 {
		limit = 20
	}
	if q.EventOffset > 0 {
		evs = evs[min(q.EventOffset, len(evs)):]
	}
	if len(evs) > limit {
		evs = evs[:limit]
	}
	rep.Events = evs
	return rep
}

// onlineAll 返回全部在线用户（按入站、用户名排序）与全部上下线记录。
func (a *App) onlineAll() OnlineReport {
	rep := OnlineReport{Running: a.xray.Running(), MetricsEnabled: a.config.MetricsPort > 0,
		IdleSeconds: int(onlineIdle.Seconds()), Users: []OnlineUser{}, Events: []OnlineEvent{}}
	now := time.Now()
	t := a.online
	t.mu.Lock()
	for _, u := range t.users {
		ou := OnlineUser{Inbound: u.Inbound, Email: u.Email, FirstSeen: u.FirstSeen.Unix(), LastSeen: u.LastSeen.Unix(),
			Conns: u.Conns, Up: u.Up, Down: u.Down, Addrs: []OnlineAddr{}}
		for _, ad := range u.Addrs {
			if now.Sub(ad.LastSeen) > onlineIdle {
				continue
			}
			oa := OnlineAddr{IP: ad.IP, FirstSeen: ad.FirstSeen.Unix(), LastSeen: ad.LastSeen.Unix(),
				Conns: ad.Conns, LastDest: ad.LastDest, Outbound: ad.Outbound}
			for n := range ad.Networks {
				oa.Networks = append(oa.Networks, n)
			}
			sort.Strings(oa.Networks)
			ou.Addrs = append(ou.Addrs, oa)
		}
		if len(ou.Addrs) == 0 {
			continue
		}
		sort.Slice(ou.Addrs, func(i, j int) bool { return ou.Addrs[i].LastSeen > ou.Addrs[j].LastSeen })
		rep.Users = append(rep.Users, ou)
	}
	for i := len(t.events) - 1; i >= 0; i-- {
		rep.Events = append(rep.Events, t.events[i])
	}
	t.mu.Unlock()

	a.store.Lock()
	for i := range rep.Users {
		u := &rep.Users[i]
		ib, label, proto := a.describeInbound(u.Inbound)
		u.InboundLabel, u.Protocol, u.User = label, proto, userDisplay(ib, u.Email)
		u.HasTraffic = u.Email != ""
		for j := range u.Addrs {
			u.Addrs[j].Outbound = a.OutboundLabel(u.Addrs[j].Outbound)
		}
	}
	for i := range rep.Events {
		a.fillEvent(&rep.Events[i])
	}
	a.store.Unlock()
	if a.traffic != nil {
		for i := range rep.Users {
			if u := &rep.Users[i]; u.Email != "" {
				if v, ok := a.traffic.todayUser(u.Email); ok {
					u.TodayUp, u.TodayDown = v.Up, v.Down
				}
			}
		}
	}
	sort.Slice(rep.Users, func(i, j int) bool {
		if rep.Users[i].InboundLabel != rep.Users[j].InboundLabel {
			return rep.Users[i].InboundLabel < rep.Users[j].InboundLabel
		}
		return rep.Users[i].User+rep.Users[i].Addrs[0].IP < rep.Users[j].User+rep.Users[j].Addrs[0].IP
	})
	return rep
}

// onlineNotifyWindow 内的上下线事件合并成一条通知，避免用户多时（或面板重启后所有人同时“上线”）刷屏。
var onlineNotifyWindow = 15 * time.Second

const maxNotifyLines = 30

type notifyBatch struct {
	mu      sync.Mutex
	pending []OnlineEvent
	timer   *time.Timer
}

// queueOnlineNotify 只持有批次锁，可以在 Xray 输出管道的回调里调用。
func (a *App) queueOnlineNotify(evs []OnlineEvent) {
	b := a.onlineBatch
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, evs...)
	if b.timer == nil {
		b.timer = time.AfterFunc(onlineNotifyWindow, a.flushOnlineNotify)
	}
}

func (a *App) flushOnlineNotify() {
	b := a.onlineBatch
	b.mu.Lock()
	evs := b.pending
	b.pending, b.timer = nil, nil
	b.mu.Unlock()
	if len(evs) > 0 {
		a.notifyOnline(evs)
	}
}

func (ev *OnlineEvent) who() string {
	if ev.User == "" {
		return "未鉴权用户"
	}
	return ev.User
}

// notifyOnline 把上下线事件发到通知渠道：同类只有一条时发详细信息，多条时合并成列表。
func (a *App) notifyOnline(evs []OnlineEvent) {
	a.store.Lock()
	for i := range evs {
		a.fillEvent(&evs[i])
	}
	a.store.Unlock()
	var ons, offs []OnlineEvent
	for _, ev := range evs {
		if ev.Kind == "offline" {
			offs = append(offs, ev)
		} else {
			ons = append(ons, ev)
		}
	}
	for _, group := range [][]OnlineEvent{ons, offs} {
		switch {
		case len(group) == 1:
			a.notifyOnlineOne(group[0])
		case len(group) > 1:
			a.notifyOnlineMany(group)
		}
	}
}

func (a *App) notifyOnlineMany(evs []OnlineEvent) {
	event, title := "user_online", ""
	var lines []string
	if evs[0].Kind == "offline" {
		event, title = "user_offline", fmt.Sprintf("%d 位用户下线", len(evs))
	} else {
		users, ips := 0, 0
		for _, e := range evs {
			if e.Kind == "online" {
				users++
			} else {
				ips++
			}
		}
		switch {
		case ips == 0:
			title = fmt.Sprintf("%d 位用户上线", users)
		case users == 0:
			title = fmt.Sprintf("%d 个用户从新地址连接", ips)
		default:
			title = fmt.Sprintf("%d 位用户上线，%d 个新地址", users, ips)
		}
	}
	for i, e := range evs {
		if i == maxNotifyLines {
			lines = append(lines, fmt.Sprintf("……另有 %d 条，详见面板「在线连接」", len(evs)-maxNotifyLines))
			break
		}
		l := fmt.Sprintf("%s · %s · %s", e.who(), e.InboundLabel, e.IP)
		switch e.Kind {
		case "new_ip":
			l += "（新地址）"
		case "offline":
			l += " · 在线 " + fmtDuration(e.Duration)
			if e.Up+e.Down > 0 {
				l += " · " + fmtBytes(e.Up+e.Down)
			}
		}
		lines = append(lines, l)
	}
	lines = append(lines, "时间："+time.Unix(evs[len(evs)-1].Time, 0).Format("2006-01-02 15:04:05"))
	a.sendNotify(event, title, strings.Join(lines, "\n"))
}

func (a *App) notifyOnlineOne(ev OnlineEvent) {
	who := ev.User
	if who == "" {
		who = "未鉴权用户（按 IP 区分）"
	}
	lines := []string{"用户：" + who, "入站：" + ev.InboundLabel}
	if proto := ev.Protocol; proto != "" {
		if ev.Network != "" {
			proto += " / " + strings.ToUpper(ev.Network)
		}
		lines = append(lines, "协议："+proto)
	}
	lines = append(lines, "地址："+ev.IP)
	at := time.Unix(ev.Time, 0).Format("2006-01-02 15:04:05")
	switch ev.Kind {
	case "online":
		a.sendNotify("user_online", "用户上线", strings.Join(append(lines, "时间："+at), "\n"))
	case "new_ip":
		a.sendNotify("user_online", "用户从新地址连接", strings.Join(append(lines, "时间："+at, "（该用户已在其他地址在线）"), "\n"))
	case "offline":
		lines = append(lines, "在线时长："+fmtDuration(ev.Duration))
		if ev.Up+ev.Down > 0 {
			lines = append(lines, fmt.Sprintf("本次流量：↑ %s ↓ %s", fmtBytes(ev.Up), fmtBytes(ev.Down)))
		}
		lines = append(lines, "时间："+at, fmt.Sprintf("（%d 分钟内没有新连接和流量）", int(onlineIdle.Minutes())))
		a.sendNotify("user_offline", "用户下线", strings.Join(lines, "\n"))
	}
}

func fmtDuration(sec int64) string {
	switch {
	case sec < 60:
		return "不到 1 分钟"
	case sec < 3600:
		return fmt.Sprintf("%d 分钟", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%d 小时 %d 分钟", sec/3600, sec%3600/60)
	}
	return fmt.Sprintf("%d 天 %d 小时", sec/86400, sec%86400/3600)
}

func fmtBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
