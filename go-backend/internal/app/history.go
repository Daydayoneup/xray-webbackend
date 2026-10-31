package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

const maxHistory = 10

// HistoryEntry 是一次成功应用的快照：面板数据（不含密码）+ 生成的配置。
type HistoryEntry struct {
	ID       string            `json:"id"`
	Time     int64             `json:"time"`
	Note     string            `json:"note"`
	Summary  string            `json:"summary"`
	Warnings []string          `json:"warnings,omitempty"`
	State    *model.PanelState `json:"state,omitempty"`
	Config   json.RawMessage   `json:"config,omitempty"`
}

func (a *App) historyDir() string { return filepath.Join(a.config.DataDir, "history") }

// snapshotState 深拷贝当前状态并去掉密码。调用方持锁。
func (a *App) snapshotState() *model.PanelState {
	data, _ := json.Marshal(a.state)
	var cp model.PanelState
	json.Unmarshal(data, &cp)
	cp.Password = nil
	return &cp
}

func summarize(st *model.PanelState) string {
	return fmt.Sprintf("%d 个节点 · %d 个节点组 · %d 个自定义代理 · %d 条规则 · %d 个入站",
		len(st.Nodes), len(st.Balancers), len(st.Proxies), len(st.Rules), len(st.Inbounds))
}

func (a *App) saveHistory(st *model.PanelState, config []byte, warnings []string) {
	a.writeHistory(&HistoryEntry{Note: "应用配置", State: st, Config: config, Warnings: warnings})
}

func (a *App) writeHistory(e *HistoryEntry) {
	if a.config.DataDir == "" {
		return
	}
	dir := a.historyDir()
	os.MkdirAll(dir, 0700)
	now := time.Now()
	e.ID = fmt.Sprintf("%d", now.UnixNano())
	e.Time = now.Unix()
	if e.State != nil {
		e.Summary = summarize(e.State)
	}
	data, _ := json.Marshal(e)
	if err := writeAtomic(filepath.Join(dir, e.ID+".json"), data); err != nil {
		slog.Warn("保存配置历史失败", "error", err)
		return
	}
	ids := a.historyIDs()
	for len(ids) > maxHistory {
		os.Remove(filepath.Join(dir, ids[0]+".json"))
		ids = ids[1:]
	}
}

// historyIDs 按时间升序返回快照 ID。
func (a *App) historyIDs() []string {
	entries, _ := os.ReadDir(a.historyDir())
	var ids []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if len(ids[i]) != len(ids[j]) {
			return len(ids[i]) < len(ids[j])
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (a *App) loadHistory(id string) (*HistoryEntry, error) {
	if strings.ContainsAny(id, "/\\.") {
		return nil, fmt.Errorf("无效的历史 ID")
	}
	data, err := os.ReadFile(filepath.Join(a.historyDir(), id+".json"))
	if err != nil {
		return nil, fmt.Errorf("历史记录 %s 不存在", id)
	}
	var e HistoryEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("历史记录已损坏")
	}
	return &e, nil
}

// ListHistory 返回快照列表（新的在前，不含正文）。
func (a *App) ListHistory() []HistoryEntry {
	ids := a.historyIDs()
	out := []HistoryEntry{}
	for i := len(ids) - 1; i >= 0; i-- {
		if e, err := a.loadHistory(ids[i]); err == nil {
			e.State, e.Config = nil, nil
			out = append(out, *e)
		}
	}
	return out
}

func prettyJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}

// HistoryDiff 对比快照配置与当前生效配置。
func (a *App) HistoryDiff(id string) ([]service.DiffLine, error) {
	e, err := a.loadHistory(id)
	if err != nil {
		return nil, err
	}
	cur, _ := os.ReadFile(a.config.ConfigPath)
	return service.LineDiff(prettyJSON(e.Config), prettyJSON(cur), 3), nil
}

// DraftDiff 对比当前生效配置与待应用的草稿。
func (a *App) DraftDiff() []service.DiffLine {
	a.store.Lock()
	draft, _ := json.Marshal(a.Build().Config)
	a.store.Unlock()
	cur, _ := os.ReadFile(a.config.ConfigPath)
	return service.LineDiff(prettyJSON(cur), prettyJSON(draft), 3)
}

// replaceState 用外部状态（快照/备份）替换当前数据，保留登录密码。调用方持锁。
func (a *App) replaceState(st *model.PanelState) {
	pw := a.state.Password
	upgradeState(st)
	st.Password = pw
	*a.state = *st
}

// RestoreHistory 恢复到某次快照的面板数据并立即应用。
func (a *App) RestoreHistory(id string) (*ApplyResult, error) {
	e, err := a.loadHistory(id)
	if err != nil {
		return nil, err
	}
	if e.State == nil {
		return nil, fmt.Errorf("该记录没有保存面板数据，无法恢复")
	}
	a.store.Lock()
	a.writeHistory(&HistoryEntry{Note: "回滚前自动备份", State: a.snapshotState(), Config: mustRead(a.config.ConfigPath)})
	a.replaceState(e.State)
	a.Persist()
	a.store.Unlock()
	return a.Apply()
}

func mustRead(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}
