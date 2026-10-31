package app

import (
	"fmt"
	"sort"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// GroupMember 是节点组成员的实时状态。
type GroupMember struct {
	Tag    string `json:"tag"`
	Label  string `json:"label"`
	Probed bool   `json:"probed"` // 是否有 Xray 探测结果
	Alive  bool   `json:"alive"`
	Delay  int64  `json:"delay"`
}

// GroupStatus 是节点组的实时状态。Current 为当前（预计）使用的成员。
type GroupStatus struct {
	Tag        string        `json:"tag"`
	Name       string        `json:"name"`
	Strategy   string        `json:"strategy"`
	Members    []GroupMember `json:"members"`
	Candidates int           `json:"candidates"`
	Alive      int           `json:"alive"`
	Probed     int           `json:"probed"`
	Current    string        `json:"current"`
	CurrentTip string        `json:"current_tip"`
	Reason     string        `json:"reason,omitempty"`
	HasMetrics bool          `json:"has_metrics"`
}

// GroupStatuses 结合生成结果与 Xray 探测数据给出每个节点组的状态。
func (a *App) GroupStatuses() []GroupStatus {
	live := a.LiveMetrics()
	a.store.Lock()
	defer a.store.Unlock()
	res := a.Build()
	out := []GroupStatus{}
	for i := range a.state.Balancers {
		b := &a.state.Balancers[i]
		g := GroupStatus{Tag: b.Tag, Name: b.Name, Strategy: b.Strategy, Reason: res.Unavailable[b.Tag], HasMetrics: live != nil}
		if g.Strategy == "" {
			g.Strategy = "leastPing"
		}
		g.Candidates = len(service.GroupCandidates(a.state, b))
		for _, m := range res.Members[b.Tag] {
			gm := GroupMember{Tag: m, Label: a.OutboundLabel(m)}
			if live != nil {
				if p, ok := live.Observatory[m]; ok {
					gm.Probed, gm.Alive, gm.Delay = true, p.Alive, p.Delay
				}
			}
			if gm.Probed {
				g.Probed++
				if gm.Alive {
					g.Alive++
				}
			}
			g.Members = append(g.Members, gm)
		}
		switch g.Strategy {
		case "select":
			g.Current = res.Selected[b.Tag]
			g.CurrentTip = "手动选择"
		case "leastPing":
			best := -1
			for k, m := range g.Members {
				if m.Alive && (best < 0 || m.Delay < g.Members[best].Delay) {
					best = k
				}
			}
			if best >= 0 {
				g.Current = g.Members[best].Tag
				g.CurrentTip = fmt.Sprintf("延迟最低 %dms", g.Members[best].Delay)
			} else if live == nil {
				g.CurrentTip = "等待 Xray 探测结果"
			} else {
				g.CurrentTip = "没有探测成功的节点"
			}
		default:
			g.CurrentTip = fmt.Sprintf("在 %d 个可用节点间轮换", len(g.Members))
		}
		sort.SliceStable(g.Members, func(x, y int) bool {
			mx, my := g.Members[x], g.Members[y]
			if mx.Alive != my.Alive {
				return mx.Alive
			}
			return mx.Alive && mx.Delay < my.Delay
		})
		out = append(out, g)
	}
	return out
}

// SelectGroupMember 切换手动选择组的节点并立即应用。
func (a *App) SelectGroupMember(tag, member string) (*ApplyResult, error) {
	a.store.Lock()
	var b *model.Balancer
	for i := range a.state.Balancers {
		if a.state.Balancers[i].Tag == tag {
			b = &a.state.Balancers[i]
		}
	}
	if b == nil {
		a.store.Unlock()
		return nil, fmt.Errorf("节点组 %s 不存在", tag)
	}
	if b.Strategy != "select" {
		a.store.Unlock()
		return nil, fmt.Errorf("只有「手动选择」类型的节点组可以切换节点")
	}
	if !containsString(service.GroupCandidates(a.state, b), member) {
		a.store.Unlock()
		return nil, fmt.Errorf("%s 不是该组的成员", member)
	}
	b.Selected = member
	a.Persist()
	a.store.Unlock()
	return a.Apply()
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
