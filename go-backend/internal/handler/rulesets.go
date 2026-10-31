package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xray-panel/internal/app"
	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

type ruleSetView struct {
	model.RuleSet
	Refs        []int     `json:"refs"`      // 引用它的规则序号（从 1 开始）
	RefRules    []ruleRef `json:"ref_rules"` // 同上，附规则名称
	DomainCount int       `json:"domain_count"`
	IPCount     int       `json:"ip_count"`
	Truncated   bool      `json:"truncated"` // 订阅分组只返回前若干条预览，完整内容用 /entries 查看
}

const ruleSetPreview = 20

type ruleRef struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
}

func newRuleSetView(st *model.PanelState, rs model.RuleSet) ruleSetView {
	v := ruleSetView{RuleSet: rs, Refs: []int{}, RefRules: []ruleRef{}, DomainCount: len(rs.Domains), IPCount: len(rs.IPs)}
	if v.Domains == nil {
		v.Domains = []string{}
	}
	if v.IPs == nil {
		v.IPs = []string{}
	}
	if rs.Source != nil && len(rs.Domains)+len(rs.IPs) > ruleSetPreview {
		v.Truncated = true
		v.Domains = v.Domains[:min(len(v.Domains), ruleSetPreview)]
		v.IPs = v.IPs[:min(len(v.IPs), ruleSetPreview-len(v.Domains))]
	}
	for i, r := range st.Rules {
		if r.Type == "set" && r.Value == rs.Tag {
			v.Refs = append(v.Refs, i+1)
			v.RefRules = append(v.RefRules, ruleRef{Index: i + 1, Name: r.Name})
		}
	}
	return v
}

func (s *Server) ListRuleSets(w http.ResponseWriter, r *http.Request) {
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	out := []ruleSetView{}
	for _, rs := range st.RuleSets {
		out = append(out, newRuleSetView(st, rs))
	}
	writeJSON(w, 200, out)
}

func (s *Server) CreateRuleSet(w http.ResponseWriter, r *http.Request) { s.saveRuleSet(w, r, "") }
func (s *Server) UpdateRuleSet(w http.ResponseWriter, r *http.Request) {
	s.saveRuleSet(w, r, chi.URLParam(r, "tag"))
}

func (s *Server) saveRuleSet(w http.ResponseWriter, r *http.Request, tag string) {
	var body model.RuleSetIn
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validate.Struct(body); err != nil {
		writeError(w, 400, translateValidation(err))
		return
	}
	rs := model.RuleSet{Name: strings.TrimSpace(body.Name), Remark: strings.TrimSpace(body.Remark)}
	if rs.Name == "" {
		writeError(w, 400, "请填写分组名称")
		return
	}
	if src := body.Source; src != nil && strings.TrimSpace(src.URL) != "" {
		if err := s.prepareRemoteSet(&rs, src, tag); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	} else {
		var err error
		if rs.Domains, err = service.NormalizeSetEntries(body.Domains, service.NormalizeSetDomain); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if rs.IPs, err = service.NormalizeSetEntries(body.IPs, service.NormalizeSetIP); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}

	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	idx := -1
	for i, x := range st.RuleSets {
		if x.Tag == tag && tag != "" {
			idx = i
		} else if x.Name == rs.Name {
			writeError(w, 400, fmt.Sprintf("已有同名分组「%s」", rs.Name))
			return
		}
	}
	code := 200
	if tag == "" {
		seq := st.RuleSetSeq
		rs.Tag = nextTag("set-", &seq, func(t string) bool { return findSet(st, t) >= 0 })
		st.RuleSetSeq = seq
		st.RuleSets = append(st.RuleSets, rs)
		code = 201
	} else if idx < 0 {
		writeError(w, 404, "分组不存在")
		return
	} else {
		rs.Tag = tag
		st.RuleSets[idx] = rs
	}
	if err := s.App.Persist(); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, code, newRuleSetView(st, rs))
}

// prepareRemoteSet 填充订阅分组：地址或格式变化（或新建）时先下载解析，成功才保存；否则沿用已有内容。
// 下载可能很慢，不持有 Store 锁。
func (s *Server) prepareRemoteSet(rs *model.RuleSet, src *model.RuleSetSourceIn, tag string) error {
	rs.Source = &model.RuleSetSource{URL: strings.TrimSpace(src.URL), Format: selOr(src.Format, "auto"), UpdateHours: src.UpdateHours}
	var prev *model.RuleSet
	if tag != "" {
		s.App.Store().Lock()
		if i := findSet(s.App.State(), tag); i >= 0 {
			p := s.App.State().RuleSets[i]
			prev = &p
		}
		s.App.Store().Unlock()
	}
	if prev != nil && prev.Source != nil && prev.Source.URL == rs.Source.URL && prev.Source.Format == rs.Source.Format {
		hours := rs.Source.UpdateHours
		*rs.Source = *prev.Source
		rs.Source.UpdateHours = hours
		rs.Domains, rs.IPs = prev.Domains, prev.IPs
		return nil
	}
	parsed, err := s.App.FetchRuleList(rs.Source.URL, rs.Source.Format)
	if err != nil {
		return err
	}
	app.ApplyParsedList(rs, parsed, time.Now())
	return nil
}

// RefreshRuleSet 立即更新订阅分组。失败时返回 400，原有内容保留。
func (s *Server) RefreshRuleSet(w http.ResponseWriter, r *http.Request) {
	rs, err := s.App.RefreshRuleSet(chi.URLParam(r, "tag"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	writeJSON(w, 200, newRuleSetView(s.App.State(), *rs))
}

// RuleSetEntries 查看分组的完整内容（订阅分组可能有上万条）：q 过滤，limit 上限。
func (s *Server) RuleSetEntries(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	i := findSet(s.App.State(), chi.URLParam(r, "tag"))
	if i < 0 {
		writeError(w, 404, "分组不存在")
		return
	}
	rs := s.App.State().RuleSets[i]
	out, total := []string{}, 0
	for _, list := range [][]string{rs.Domains, rs.IPs} {
		for _, e := range list {
			if q != "" && !strings.Contains(e, q) {
				continue
			}
			total++
			if len(out) < limit {
				out = append(out, e)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"entries": out, "total": total})
}

func findSet(st *model.PanelState, tag string) int {
	for i, x := range st.RuleSets {
		if x.Tag == tag {
			return i
		}
	}
	return -1
}

// DeleteRuleSet 删除分组，同时删除引用它的规则（与删除自定义代理一致）。
func (s *Server) DeleteRuleSet(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	s.App.Store().Lock()
	defer s.App.Store().Unlock()
	st := s.App.State()
	i := findSet(st, tag)
	if i < 0 {
		writeError(w, 404, "分组不存在")
		return
	}
	st.RuleSets = append(st.RuleSets[:i:i], st.RuleSets[i+1:]...)
	before := len(st.Rules)
	s.App.Persist()
	writeJSON(w, 200, map[string]any{"ok": true, "removed_rules": before - len(st.Rules)})
}

// GeoCatalog 列出规则数据库中的分类：kind=site|ip。
func (s *Server) GeoCatalog(w http.ResponseWriter, r *http.Request) {
	codes, err := s.App.GeoDB().Catalog(r.URL.Query().Get("kind"))
	if err != nil {
		writeJSON(w, 200, map[string]any{"available": false, "error": err.Error(), "codes": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"available": true, "codes": codes})
}

// GeoEntries 返回某个分类的条目：kind、code、q（过滤）、limit。
func (s *Server) GeoEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	entries, total, err := s.App.GeoDB().Entries(q.Get("kind"), q.Get("code"), q.Get("q"), limit)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"entries": entries, "total": total})
}
