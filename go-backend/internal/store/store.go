package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"xray-panel/internal/model"
)

// Store is the persistence interface for PanelState.
type Store interface {
	Load() (*model.PanelState, error)
	Save(state *model.PanelState) error
	Lock()
	Unlock()
}

// ---------- JSON implementation ----------

type jsonStore struct {
	path      string
	mu        sync.Mutex
	socksPort int
	httpPort  int
}

func NewJSONStore(path string, socksPort, httpPort int) Store {
	return &jsonStore{path: path, socksPort: socksPort, httpPort: httpPort}
}

func (s *jsonStore) Lock()   { s.mu.Lock() }
func (s *jsonStore) Unlock() { s.mu.Unlock() }

func (s *jsonStore) Load() (*model.PanelState, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return migrate(nil, s.socksPort, s.httpPort), nil
		}
		return nil, fmt.Errorf("读取状态文件失败: %w", err)
	}
	var dict map[string]any
	if err := json.Unmarshal(raw, &dict); err != nil {
		return nil, fmt.Errorf("解析状态文件失败: %w", err)
	}
	return migrate(dict, s.socksPort, s.httpPort), nil
}

func (s *jsonStore) Save(state *model.PanelState) error {
	dir := filepath.Dir(s.path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("原子替换失败: %w", err)
	}
	return nil
}

// ---------- migration ----------

func migrate(raw map[string]any, socksPort, httpPort int) *model.PanelState {
	if raw == nil {
		raw = map[string]any{}
	}
	if _, ok := raw["inbounds"]; !ok {
		raw["inbounds"] = defaultInbounds(socksPort, httpPort)
	} else if raw["inbounds"] == nil {
		raw["inbounds"] = []any{}
	}
	for _, key := range []string{"proxies", "balancers", "rules"} {
		if raw[key] == nil {
			raw[key] = []any{}
		}
	}
	raw["rules"] = fixRules(raw["rules"].([]any))
	setDefaultSeq(raw, "inbound_seq", raw["inbounds"].([]any), "in-")
	setDefaultSeq(raw, "proxy_seq", raw["proxies"].([]any), "px-")
	setDefaultSeq(raw, "balancer_seq", raw["balancers"].([]any), "auto-")

	// 迁移旧单订阅格式 → 多订阅数组
	migrateSubscriptions(raw)

	data, _ := json.Marshal(raw)
	var state model.PanelState
	json.Unmarshal(data, &state)
	return &state
}

func defaultInbounds(socksPort, httpPort int) []any {
	return []any{
		map[string]any{"tag": "in-0", "protocol": "socks", "listen": "0.0.0.0", "port": socksPort, "udp": true, "auth": nil},
		map[string]any{"tag": "in-1", "protocol": "http", "listen": "0.0.0.0", "port": httpPort, "auth": nil},
	}
}

func fixRules(raw []any) []any {
	nextID := 1
	var out []any
	for _, r := range raw {
		m := asMap(r)
		if m["id"] == nil || m["id"].(float64) == 0 {
			m["id"] = float64(nextID)
		}
		if _, ok := m["enabled"]; !ok {
			m["enabled"] = true
		}
		if v := int(m["id"].(float64)); v >= nextID {
			nextID = v + 1
		}
		out = append(out, m)
	}
	return out
}

func setDefaultSeq(raw map[string]any, key string, items []any, prefix string) {
	if raw[key] != nil {
		return
	}
	raw[key] = float64(seqFromTags(items, prefix))
}

func seqFromTags(items []any, prefix string) int {
	mx := -1
	for _, it := range items {
		m := asMap(it)
		tag, _ := m["tag"].(string)
		if strings.HasPrefix(tag, prefix) {
			n, err := strconv.Atoi(tag[len(prefix):])
			if err == nil && n > mx {
				mx = n
			}
		}
	}
	return mx + 1
}

func asMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func migrateSubscriptions(raw map[string]any) {
	// 已经是新格式（subscriptions 数组）
	if _, ok := raw["subscriptions"]; ok {
		delete(raw, "subscription") // 清除旧 key
		setDefaultSeq(raw, "sub_seq", raw["subscriptions"].([]any), "")
		return
	}
	// 旧格式: subscription 是单个对象 → 转成 subscriptions 数组
	if old, ok := raw["subscription"].(map[string]any); ok && len(old) > 0 {
		if old["url"] != nil && old["url"] != "" {
			old["id"] = float64(0)
			raw["subscriptions"] = []any{old}
		} else {
			raw["subscriptions"] = []any{}
		}
	} else {
		raw["subscriptions"] = []any{}
	}
	delete(raw, "subscription")
	setDefaultSeq(raw, "sub_seq", raw["subscriptions"].([]any), "")
}
