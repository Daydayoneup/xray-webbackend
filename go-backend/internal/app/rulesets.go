package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"xray-panel/internal/model"
	"xray-panel/internal/service"
)

// SetListFetcher 替换远程规则列表的下载函数（测试用）。
func (a *App) SetListFetcher(f func(url string) ([]byte, error)) { a.fetchList = f }

// downloadRuleList 先直连，失败后经本机 HTTP 入站代理重试（jsDelivr / GitHub 在部分网络直连不通）。
func (a *App) downloadRuleList(url string) ([]byte, error) {
	var data []byte
	err := a.withFallback(func(c *http.Client) error {
		var err error
		data, err = service.FetchRuleList(c, url, a.config.SubscriptionAllowInternal)
		return err
	})
	return data, err
}

// FetchRuleList 下载并解析远程规则列表。调用方不要持有 Store 锁（下载可能很慢）。
func (a *App) FetchRuleList(url, format string) (*service.ParsedList, error) {
	data, err := a.fetchList(url)
	if err != nil {
		return nil, fmt.Errorf("下载失败：%v", err)
	}
	return service.ParseRuleList(data, format)
}

// ApplyParsedList 把解析结果写入分组的内容与来源信息。
func ApplyParsedList(rs *model.RuleSet, p *service.ParsedList, now time.Time) {
	rs.Domains, rs.IPs = p.Domains, p.IPs
	if rs.IPs == nil {
		rs.IPs = []string{}
	}
	rs.Source.UpdatedAt, rs.Source.TriedAt, rs.Source.LastError = now.Unix(), now.Unix(), ""
	rs.Source.Detected, rs.Source.Skipped = p.Format, p.Skipped
}

// RefreshRuleSet 立即更新一个订阅分组；失败时保留原有内容并记录错误。
func (a *App) RefreshRuleSet(tag string) (*model.RuleSet, error) {
	a.store.Lock()
	var src model.RuleSetSource
	found := false
	for _, rs := range a.state.RuleSets {
		if rs.Tag == tag && rs.Source != nil {
			src, found = *rs.Source, true
		}
	}
	a.store.Unlock()
	if !found {
		return nil, fmt.Errorf("分组不存在或不是订阅分组")
	}
	parsed, ferr := a.FetchRuleList(src.URL, src.Format)

	a.store.Lock()
	defer a.store.Unlock()
	for i := range a.state.RuleSets {
		rs := &a.state.RuleSets[i]
		if rs.Tag != tag || rs.Source == nil {
			continue
		}
		now := time.Now()
		if ferr != nil {
			rs.Source.TriedAt, rs.Source.LastError = now.Unix(), ferr.Error()
		} else {
			ApplyParsedList(rs, parsed, now)
		}
		a.Persist()
		out := *rs
		return &out, ferr
	}
	return nil, fmt.Errorf("分组已被删除")
}

func (a *App) dueRuleSets(now time.Time) []string {
	a.store.Lock()
	defer a.store.Unlock()
	var tags []string
	for _, rs := range a.state.RuleSets {
		s := rs.Source
		if s == nil || s.UpdateHours <= 0 {
			continue
		}
		last := max(s.TriedAt, s.UpdatedAt)
		if now.Unix()-last >= int64(s.UpdateHours)*3600 {
			tags = append(tags, rs.Tag)
		}
	}
	return tags
}

func (a *App) refreshDueRuleSets(tags []string) {
	for _, t := range tags {
		if rs, err := a.RefreshRuleSet(t); err != nil {
			slog.Warn("分组自动更新失败", "set", t, "error", err)
		} else {
			slog.Info("分组自动更新完成", "set", rs.Name, "domains", len(rs.Domains), "ips", len(rs.IPs))
		}
	}
}
