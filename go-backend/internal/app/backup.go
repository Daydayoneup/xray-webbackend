package app

import (
	"encoding/json"
	"fmt"
	"time"

	"xray-panel/internal/store"
)

// Backup 是导出文件的格式。
type Backup struct {
	App        string          `json:"app"`
	Version    int             `json:"version"`
	ExportedAt int64           `json:"exported_at"`
	State      json.RawMessage `json:"state"`
}

// Export 导出全部面板数据（不含登录密码）。
func (a *App) Export() ([]byte, error) {
	a.store.Lock()
	st := a.snapshotState()
	a.store.Unlock()
	data, _ := json.Marshal(st)
	return json.MarshalIndent(Backup{App: "xray-panel", Version: st.SchemaVersion, ExportedAt: time.Now().Unix(), State: data}, "", "  ")
}

// Import 导入备份文件（也接受直接的 panel.json）。导入前自动保存一份历史快照；导入后需手动应用。
func (a *App) Import(data []byte) (string, error) {
	var b Backup
	raw := data
	if json.Unmarshal(data, &b) == nil && len(b.State) > 0 {
		raw = b.State
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("文件不是有效的 JSON")
	}
	_, hasNodes := m["nodes"]
	_, hasInbounds := m["inbounds"]
	_, hasRules := m["rules"]
	if !hasNodes && !hasInbounds && !hasRules {
		return "", fmt.Errorf("文件中没有面板数据（不是本面板导出的备份）")
	}
	st := store.Normalize(m, 10808, 10809)
	if v, ok := m["schema_version"].(float64); ok {
		st.SchemaVersion = int(v)
	}

	a.store.Lock()
	defer a.store.Unlock()
	a.writeHistory(&HistoryEntry{Note: "导入备份前自动备份", State: a.snapshotState(), Config: mustRead(a.config.ConfigPath)})
	a.replaceState(st)
	if err := a.Persist(); err != nil {
		return "", err
	}
	return summarize(a.state), nil
}
