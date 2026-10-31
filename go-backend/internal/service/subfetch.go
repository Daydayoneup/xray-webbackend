package service

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xray-panel/internal/model"
)

const subUA = "Mozilla/5.0 (X11; Linux x86_64) Shadowrocket/2.2.49"

// maxSubSize 订阅响应体上限，防止异常响应耗尽内存。
const maxSubSize = 10 << 20

type FetchedSub struct {
	Nodes   []NodeRaw
	Skipped []SkipInfo
	Meta    map[string]string
	Usage   *model.SubUsage
}

// FetchSubscription 拉取并解析订阅。返回的错误信息直接面向用户。
func FetchSubscription(urlStr string, allowInternal bool) (*FetchedSub, error) {
	body, header, err := fetchURL(urlStr, allowInternal)
	if err != nil {
		return nil, err
	}
	if LooksLikeClash(body) {
		return nil, fmt.Errorf("这是 Clash 格式的订阅，暂不支持；请在机场后台复制「通用 / V2Ray」订阅链接")
	}
	links, meta := ExtractLinks(body)
	if len(links) == 0 {
		return nil, fmt.Errorf("订阅内容中没有找到节点链接（可能是链接已失效或需要更换订阅格式）")
	}
	nodes, skipped := ParseLinks(links)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("未解析到任何 Xray 可用节点（跳过: %s）", SummarizeSkipped(skipped))
	}
	return &FetchedSub{Nodes: nodes, Skipped: skipped, Meta: meta, Usage: ParseUserinfo(header.Get("subscription-userinfo"))}, nil
}

// ParseUserinfo 解析 "upload=1; download=2; total=3; expire=4"。
func ParseUserinfo(h string) *model.SubUsage {
	if strings.TrimSpace(h) == "" {
		return nil
	}
	u := &model.SubUsage{}
	found := false
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil {
			continue
		}
		v := int64(n)
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			u.Upload, found = v, true
		case "download":
			u.Download, found = v, true
		case "total":
			u.Total, found = v, true
		case "expire":
			u.Expire, found = v, true
		}
	}
	if !found {
		return nil
	}
	return u
}

func fetchURL(urlStr string, allowInternal bool) (string, http.Header, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", nil, fmt.Errorf("链接格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("只支持 http/https 订阅链接")
	}
	if u.Hostname() == "" {
		return "", nil, fmt.Errorf("订阅链接缺少主机名")
	}
	if !allowInternal {
		ips, err := net.LookupIP(u.Hostname())
		if err != nil {
			return "", nil, fmt.Errorf("无法解析订阅域名 %s", u.Hostname())
		}
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
				ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return "", nil, fmt.Errorf("订阅域名解析到内网/保留地址 %s,已拒绝", ip)
			}
		}
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.Header.Set("User-Agent", subUA)
	resp, err := client.Do(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok && ue.Timeout() {
			return "", nil, fmt.Errorf("连接订阅服务器超时")
		}
		return "", nil, fmt.Errorf("连接订阅服务器失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "", nil, fmt.Errorf("订阅服务器返回重定向(HTTP %d)，出于安全考虑不跟随，请填写最终地址", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("订阅服务器返回 HTTP %d（链接可能已失效或被限制）", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSubSize+1))
	if err != nil {
		return "", nil, fmt.Errorf("读取订阅内容失败: %v", err)
	}
	if len(data) > maxSubSize {
		return "", nil, fmt.Errorf("订阅内容超过 10MB，已拒绝")
	}
	return string(data), resp.Header, nil
}
