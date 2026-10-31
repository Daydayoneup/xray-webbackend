package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"xray-panel/internal/service"
)

const keepTrafficDays = 92

type trafficDay struct {
	Inbound  map[string]service.Traffic `json:"inbound"`
	Outbound map[string]service.Traffic `json:"outbound"`
	User     map[string]service.Traffic `json:"user"`
}

func newDay() *trafficDay {
	return &trafficDay{Inbound: map[string]service.Traffic{}, Outbound: map[string]service.Traffic{}, User: map[string]service.Traffic{}}
}

// trafficStore 把 Xray 自启动以来的累计计数转换成按天的增量并持久化。
type trafficStore struct {
	mu     sync.Mutex
	path   string
	Days   map[string]*trafficDay `json:"days"`
	last   map[string]service.Traffic
	live   *service.Metrics
	liveAt time.Time
	err    string
}

func loadTrafficStore(dataDir string) *trafficStore {
	t := &trafficStore{path: filepath.Join(dataDir, "traffic.json"), Days: map[string]*trafficDay{}, last: map[string]service.Traffic{}}
	if data, err := os.ReadFile(t.path); err == nil {
		json.Unmarshal(data, t)
		if t.Days == nil {
			t.Days = map[string]*trafficDay{}
		}
	}
	t.normalizeUsers()
	return t
}

// normalizeUsers 把 v6 之前的用户 key（name@入站tag）合并为用户名。
func (t *trafficStore) normalizeUsers() {
	for _, d := range t.Days {
		if d == nil {
			continue
		}
		for k, v := range d.User {
			if name, tag, ok := strings.Cut(k, "@"); ok && strings.HasPrefix(tag, "in-") {
				cur := d.User[name]
				d.User[name] = service.Traffic{Up: cur.Up + v.Up, Down: cur.Down + v.Down}
				delete(d.User, k)
			}
		}
	}
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// trafficDelta 是两次采样之间的增量。
type trafficDelta struct {
	Inbound map[string]service.Traffic
	User    map[string]service.Traffic
}

// add 记录一次采样并返回本次增量；计数变小视为 Xray 重启（计数清零）。
func (t *trafficStore) add(m *service.Metrics, now time.Time) trafficDelta {
	delta := trafficDelta{Inbound: map[string]service.Traffic{}, User: map[string]service.Traffic{}}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.live, t.liveAt, t.err = m, now, ""
	day := t.Days[dayKey(now)]
	if day == nil {
		day = newDay()
		t.Days[dayKey(now)] = day
	}
	acc := func(kind string, src, dst, out map[string]service.Traffic) {
		for tag, v := range src {
			key := kind + "|" + tag
			prev, ok := t.last[key]
			d := v
			if ok && v.Up >= prev.Up && v.Down >= prev.Down {
				d = service.Traffic{Up: v.Up - prev.Up, Down: v.Down - prev.Down}
			}
			t.last[key] = v
			if d.Up == 0 && d.Down == 0 {
				continue
			}
			cur := dst[tag]
			dst[tag] = service.Traffic{Up: cur.Up + d.Up, Down: cur.Down + d.Down}
			if out != nil {
				out[tag] = d
			}
		}
	}
	acc("in", m.Inbound, day.Inbound, delta.Inbound)
	acc("out", m.Outbound, day.Outbound, nil)
	acc("user", m.User, day.User, delta.User)
	cutoff := dayKey(now.AddDate(0, 0, -keepTrafficDays))
	for k := range t.Days {
		if k < cutoff {
			delete(t.Days, k)
		}
	}
	return delta
}

// todayUser 返回某个用户今天的流量。
func (t *trafficStore) todayUser(email string) (service.Traffic, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	day := t.Days[dayKey(time.Now())]
	if day == nil {
		return service.Traffic{}, false
	}
	v, ok := day.User[email]
	return v, ok
}

func (t *trafficStore) save() {
	t.mu.Lock()
	data, _ := json.Marshal(t)
	t.mu.Unlock()
	writeAtomic(t.path, data)
}

// TrafficRow 是某个对象在统计区间内的流量。
type TrafficRow struct {
	Tag   string `json:"tag"`
	Label string `json:"label"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

type TrafficDaily struct {
	Date string `json:"date"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

type TrafficReport struct {
	Enabled  bool           `json:"enabled"`
	Error    string         `json:"error,omitempty"`
	Days     int            `json:"days"`
	Total    TrafficDaily   `json:"total"`
	Daily    []TrafficDaily `json:"daily"`
	Outbound []TrafficRow   `json:"outbound"`
	Inbound  []TrafficRow   `json:"inbound"`
	User     []TrafficRow   `json:"user"`
	LiveAt   int64          `json:"live_at"`
}

// Traffic 汇总最近 days 天（含今天）的流量。出站流量以“经过该出口”的流量计，
// Total 用入站流量合计（避免链式代理重复计算）。
func (a *App) Traffic(days int) TrafficReport {
	rep := TrafficReport{Enabled: a.config.MetricsPort > 0 && a.traffic != nil, Days: days,
		Daily: []TrafficDaily{}, Outbound: []TrafficRow{}, Inbound: []TrafficRow{}, User: []TrafficRow{}}
	if !rep.Enabled {
		return rep
	}
	t := a.traffic
	t.mu.Lock()
	defer t.mu.Unlock()
	rep.Error = t.err
	rep.LiveAt = t.liveAt.Unix()
	sums := map[string]map[string]*TrafficRow{"in": {}, "out": {}, "user": {}}
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		k := dayKey(now.AddDate(0, 0, -i))
		d := TrafficDaily{Date: k}
		if day := t.Days[k]; day != nil {
			for kind, src := range map[string]map[string]service.Traffic{"in": day.Inbound, "out": day.Outbound, "user": day.User} {
				for tag, v := range src {
					if kind == "in" && tag == service.MetricsInboundTag {
						continue
					}
					r := sums[kind][tag]
					if r == nil {
						r = &TrafficRow{Tag: tag}
						sums[kind][tag] = r
					}
					r.Up += v.Up
					r.Down += v.Down
					if kind == "in" {
						d.Up += v.Up
						d.Down += v.Down
					}
				}
			}
		}
		rep.Total.Up += d.Up
		rep.Total.Down += d.Down
		rep.Daily = append(rep.Daily, d)
	}
	rows := func(kind string) []TrafficRow {
		out := []TrafficRow{}
		for _, r := range sums[kind] {
			out = append(out, *r)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Up+out[i].Down > out[j].Up+out[j].Down })
		return out
	}
	a.store.Lock()
	rep.Outbound, rep.Inbound, rep.User = rows("out"), rows("in"), rows("user")
	for i := range rep.Outbound {
		rep.Outbound[i].Label = a.OutboundLabel(rep.Outbound[i].Tag)
	}
	for i := range rep.Inbound {
		rep.Inbound[i].Label = rep.Inbound[i].Tag
		for _, ib := range a.state.Inbounds {
			if ib.Tag == rep.Inbound[i].Tag {
				rep.Inbound[i].Label = selOrStr(ib.Remark, ib.Protocol) + ":" + itoa(ib.Port)
			}
		}
	}
	for i := range rep.User {
		rep.User[i].Label = rep.User[i].Tag
	}
	a.store.Unlock()
	return rep
}

// LiveMetrics 返回最近一次采样（可能为 nil）。
func (a *App) LiveMetrics() *service.Metrics {
	if a.traffic == nil {
		return nil
	}
	a.traffic.mu.Lock()
	defer a.traffic.mu.Unlock()
	if time.Since(a.traffic.liveAt) > 3*time.Minute {
		return nil
	}
	return a.traffic.live
}

func (a *App) collectMetrics() {
	if a.config.MetricsPort <= 0 || a.traffic == nil || !a.xray.Running() {
		return
	}
	m, err := service.FetchMetrics(a.config.MetricsPort)
	if err != nil {
		a.traffic.mu.Lock()
		a.traffic.err = "读取 Xray 统计失败（应用一次配置以开启统计）: " + err.Error()
		a.traffic.mu.Unlock()
		return
	}
	now := time.Now()
	d := a.traffic.add(m, now)
	a.traffic.save()
	a.online.addTraffic(d, now)
}
