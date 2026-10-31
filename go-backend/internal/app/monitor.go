package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// NotifyEvent 描述一种可订阅的提醒。
type NotifyEvent struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var NotifyEvents = []NotifyEvent{
	{"xray_crash", "Xray 崩溃 / 停止自动重启"},
	{"sub_fail", "订阅更新失败"},
	{"sub_expire", "订阅 3 天内到期"},
	{"sub_traffic", "订阅流量用量超过 90%"},
	{"group_down", "节点组的节点全部不可用"},
	{"geo_update", "规则数据库更新结果"},
	{"user_online", "用户上线（含从新地址连接）"},
	{"user_offline", "用户下线"},
}

func DefaultNotifyEvents() []string {
	out := []string{}
	for _, e := range NotifyEvents {
		out = append(out, e.ID)
	}
	return out
}

type monitorState struct {
	mu        sync.Mutex
	sent      map[string]time.Time
	lastState string
}

func newMonitorState() *monitorState { return &monitorState{sent: map[string]time.Time{}} }

func itoa(n int) string { return strconv.Itoa(n) }

func selOrStr(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (a *App) eventEnabled(id string) bool {
	for _, e := range a.state.Notify.Events {
		if e == id {
			return true
		}
	}
	return false
}

// notifyOnce 在 ttl 内同一 key 只发送一次。调用方不需要持锁。
func (a *App) notifyOnce(event, key string, ttl time.Duration, title, body string) {
	a.store.Lock()
	enabled := a.eventEnabled(event)
	cfg := a.state.Notify
	a.store.Unlock()
	if !enabled {
		return
	}
	a.monitor.mu.Lock()
	if t, ok := a.monitor.sent[key]; ok && time.Since(t) < ttl {
		a.monitor.mu.Unlock()
		return
	}
	a.monitor.sent[key] = time.Now()
	a.monitor.mu.Unlock()
	a.deliver(cfg, title, body)
}

// sendNotify 发送一条提醒（不去重）。调用方不需要持锁。
func (a *App) sendNotify(event, title, body string) {
	a.store.Lock()
	enabled := a.eventEnabled(event)
	cfg := a.state.Notify
	a.store.Unlock()
	if enabled {
		a.deliver(cfg, title, body)
	}
}

func (a *App) deliver(cfg model.NotifySettings, title, body string) {
	go func() {
		err := a.withFallback(func(c *http.Client) error { return service.Notify(cfg, c, "Xray 面板："+title, body) })
		if err != nil && err != service.ErrNoChannel {
			slog.Warn("发送通知失败", "error", err)
		}
	}()
}

// SendTestNotify 发送测试消息。
func (a *App) SendTestNotify() error {
	a.store.Lock()
	cfg := a.state.Notify
	a.store.Unlock()
	return a.withFallback(func(c *http.Client) error {
		return service.Notify(cfg, c, "Xray 面板：测试通知", "收到这条消息说明通知配置正确。")
	})
}

// checkAlerts 检查各类提醒条件。
func (a *App) checkAlerts() {
	// Xray 状态
	st := a.xray.Status()
	a.monitor.mu.Lock()
	prev := a.monitor.lastState
	a.monitor.lastState = st.State
	a.monitor.mu.Unlock()
	if prev == "running" && (st.State == "restarting" || st.State == "crashed") {
		a.notifyOnce("xray_crash", "xray:"+st.State, 10*time.Minute, "Xray 异常退出",
			fmt.Sprintf("退出原因：%s。%s", st.LastExit, map[bool]string{true: "已停止自动重启，请登录面板查看日志。", false: "正在自动重启。"}[st.State == "crashed"]))
	}

	now := time.Now()
	type subAlert struct{ event, key, title, body string }
	var alerts []subAlert
	a.store.Lock()
	for _, s := range a.state.Subscriptions {
		name := subDisplayName(&s)
		if s.LastError != "" {
			alerts = append(alerts, subAlert{"sub_fail", "subfail:" + itoa(s.ID) + s.LastError, "订阅更新失败", name + "：" + s.LastError})
		}
		if u := s.Usage; u != nil {
			if u.Expire > 0 && u.Expire > now.Unix() && u.Expire-now.Unix() < 3*86400 {
				alerts = append(alerts, subAlert{"sub_expire", "expire:" + itoa(s.ID), "订阅即将到期",
					fmt.Sprintf("%s 将于 %s 到期", name, time.Unix(u.Expire, 0).Format("2006-01-02 15:04"))})
			}
			if u.Total > 0 && float64(u.Upload+u.Download) >= 0.9*float64(u.Total) {
				alerts = append(alerts, subAlert{"sub_traffic", "traffic:" + itoa(s.ID), "订阅流量即将用完",
					fmt.Sprintf("%s 已用 %.0f%%", name, 100*float64(u.Upload+u.Download)/float64(u.Total))})
			}
		}
	}
	a.store.Unlock()
	for _, al := range alerts {
		a.notifyOnce(al.event, al.key, 24*time.Hour, al.title, al.body)
	}

	// 节点组全部不可用（依据 Xray 探测结果）
	for _, g := range a.GroupStatuses() {
		if g.Probed > 0 && g.Probed == len(g.Members) && g.Alive == 0 {
			a.notifyOnce("group_down", "group:"+g.Tag, 6*time.Hour, "节点组不可用",
				fmt.Sprintf("「%s」的 %d 个节点全部探测失败", g.Name, len(g.Members)))
		}
	}
}

// RunBackground 启动后台任务：采集流量/探测结果、在线用户掉线检测（30s）、检查提醒（60s）、geo 自动更新。
func (a *App) RunBackground(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	tick := 0
	for {
		select {
		case <-ctx.Done():
			if a.traffic != nil {
				a.traffic.save()
			}
			return
		case <-t.C:
			tick++
			a.collectMetrics()
			a.online.sweep(time.Now())
			if tick%2 == 0 {
				a.checkAlerts()
			}
			if tick%120 == 0 { // 每小时检查一次 geo 自动更新
				a.autoUpdateGeo()
			}
		}
	}
}
