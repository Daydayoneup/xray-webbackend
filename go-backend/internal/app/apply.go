package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"
)

type ApplyResult struct {
	Running  bool     `json:"xray_running"`
	Warnings []string `json:"warnings"`
}

// ApplyError 是应用失败的详情，Logs 为 Xray 最近的输出，便于用户定位原因。
type ApplyError struct {
	Msg        string   `json:"detail"`
	Logs       []string `json:"logs,omitempty"`
	RolledBack bool     `json:"rolled_back"`
	Validation bool     `json:"-"` // 配置校验失败（用户可修正）
}

func (e *ApplyError) Error() string { return e.Msg }

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Apply 以事务方式应用配置：校验 → 备份旧配置 → 写入 → 重启 → 观察存活，
// 新配置启动失败时自动回滚到上一版并恢复运行。
func (a *App) Apply() (*ApplyResult, error) {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()

	a.store.Lock()
	res := a.Build()
	snapshot := a.snapshotState()
	a.store.Unlock()

	data, _ := json.MarshalIndent(res.Config, "", "  ")
	if ok, out := a.xray.TestConfig(data); !ok {
		if len(out) > 800 {
			out = out[len(out)-800:]
		}
		return nil, &ApplyError{Msg: "配置校验失败: " + out, Validation: true}
	}

	path := a.config.ConfigPath
	prev, prevErr := os.ReadFile(path)
	if err := writeAtomic(path, data); err != nil {
		return nil, &ApplyError{Msg: fmt.Sprintf("写入配置失败: %v", err)}
	}
	if a.xray.Restart(path) {
		if prevErr == nil {
			os.WriteFile(path+".bak", prev, 0600)
		}
		a.saveHistory(snapshot, data, res.Warnings)
		return &ApplyResult{Running: true, Warnings: res.Warnings}, nil
	}

	logs := a.xray.Logs(30)
	if prevErr == nil {
		writeAtomic(path, prev)
		msg := "新配置启动失败，已自动回滚到上一版配置"
		if !a.xray.Restart(path) {
			msg += "（回滚后 Xray 仍未运行）"
		}
		slog.Warn("应用配置失败，已回滚")
		return nil, &ApplyError{Msg: msg, Logs: logs, RolledBack: true}
	}
	// 没有可回滚的版本：移除失败的配置，避免它被当作“已生效”
	a.xray.Stop()
	os.Remove(path)
	return nil, &ApplyError{Msg: "Xray 启动失败，请查看日志", Logs: logs}
}

// RunScheduler 定时检查需要自动更新的订阅。若更新前配置处于“已生效”状态且 Xray
// 在运行，更新后自动应用；否则只更新节点，等用户确认后手动应用。
func (a *App) RunScheduler(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.autoUpdate()
		}
	}
}

func (a *App) dueSubscriptions(now time.Time) []int {
	a.store.Lock()
	defer a.store.Unlock()
	var ids []int
	for _, s := range a.state.Subscriptions {
		if s.AutoUpdateHours <= 0 {
			continue
		}
		last := s.TriedAt
		if last == 0 {
			last = s.FetchedAt
		}
		if now.Unix()-last >= int64(s.AutoUpdateHours)*3600 {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func (a *App) autoUpdate() {
	ids := a.dueSubscriptions(time.Now())
	sets := a.dueRuleSets(time.Now())
	if len(ids) == 0 && len(sets) == 0 {
		return
	}
	a.store.Lock()
	wasClean := !a.Dirty(a.AppliedConfig()) && a.xray.Running()
	a.store.Unlock()

	a.refreshDueRuleSets(sets)
	var results []*RefreshResult
	if len(ids) > 0 { // RefreshAll(nil) 表示全部订阅，这里只更新到期的
		results = a.RefreshAll(ids)
	}
	for _, r := range results {
		if r.OK {
			slog.Info("订阅自动更新完成", "subscription", r.Name, "added", r.Added, "removed", r.Removed)
		} else {
			slog.Warn("订阅自动更新失败", "subscription", r.Name, "error", r.Error)
		}
	}

	a.store.Lock()
	dirty := a.Dirty(a.AppliedConfig())
	a.store.Unlock()
	if wasClean && dirty {
		if _, err := a.Apply(); err != nil {
			slog.Warn("订阅/分组更新后自动应用失败", "error", err)
		} else {
			slog.Info("订阅更新后已自动应用配置")
		}
	}
}
