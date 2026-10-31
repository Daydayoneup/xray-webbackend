package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Traffic 是累计流量（字节）。
type Traffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

// ProbeState 是 Xray observatory 对某个出站的探测结果。
type ProbeState struct {
	Alive    bool  `json:"alive"`
	Delay    int64 `json:"delay"` // 毫秒
	LastSeen int64 `json:"last_seen"`
}

// Metrics 是从 Xray metrics 接口（/debug/vars）读到的数据。计数器自 Xray 启动起累计。
type Metrics struct {
	Inbound     map[string]Traffic    `json:"inbound"`
	Outbound    map[string]Traffic    `json:"outbound"`
	User        map[string]Traffic    `json:"user"`
	Observatory map[string]ProbeState `json:"observatory"`
}

var metricsClient = &http.Client{Timeout: 3 * time.Second}

// FetchMetrics 读取 http://127.0.0.1:<port>/debug/vars。
func FetchMetrics(port int) (*Metrics, error) {
	resp, err := metricsClient.Get(fmt.Sprintf("http://127.0.0.1:%d/debug/vars", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("metrics HTTP %d", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return ParseMetrics(raw), nil
}

// ParseMetrics 解析 expvar JSON，对缺失字段宽容。
func ParseMetrics(raw map[string]json.RawMessage) *Metrics {
	m := &Metrics{Inbound: map[string]Traffic{}, Outbound: map[string]Traffic{}, User: map[string]Traffic{}, Observatory: map[string]ProbeState{}}
	var stats map[string]map[string]struct {
		Uplink   int64 `json:"uplink"`
		Downlink int64 `json:"downlink"`
	}
	if b, ok := raw["stats"]; ok {
		json.Unmarshal(b, &stats)
	}
	for kind, dst := range map[string]map[string]Traffic{"inbound": m.Inbound, "outbound": m.Outbound, "user": m.User} {
		for tag, v := range stats[kind] {
			dst[tag] = Traffic{Up: v.Uplink, Down: v.Downlink}
		}
	}
	var obs map[string]struct {
		Alive        bool  `json:"alive"`
		Delay        int64 `json:"delay"`
		LastSeenTime int64 `json:"last_seen_time"`
	}
	if b, ok := raw["observatory"]; ok {
		json.Unmarshal(b, &obs)
	}
	for tag, v := range obs {
		m.Observatory[tag] = ProbeState{Alive: v.Alive, Delay: v.Delay, LastSeen: v.LastSeenTime}
	}
	return m
}
