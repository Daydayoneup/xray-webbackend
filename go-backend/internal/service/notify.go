package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xray-panel/internal/model"
)

// 发送时间，测试中可替换
var notifyNow = time.Now

type notifyChannel struct {
	name string
	send func(c *http.Client, title, body string) error
}

// postJSON 发送 JSON；check 用于解析返回体（飞书/钉钉/企业微信出错时 HTTP 状态仍为 200）。
func postJSON(c *http.Client, u string, payload any, check func([]byte) error) error {
	data, _ := json.Marshal(payload)
	resp, err := c.Post(u, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if check != nil {
		return check(body)
	}
	return nil
}

// checkCode 检查 {"code":0} / {"errcode":0} 形式的返回。
func checkCode(field, msgField string) func([]byte) error {
	return func(b []byte) error {
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			return nil // 非 JSON 返回视为成功
		}
		code, ok := m[field].(float64)
		if !ok || code == 0 {
			return nil
		}
		msg, _ := m[msgField].(string)
		return fmt.Errorf("错误码 %d：%s", int(code), msg)
	}
}

func hmacB64(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// FeishuSign 飞书签名：以 "timestamp\nsecret" 为 key 对空串做 HmacSHA256 后 base64。
func FeishuSign(ts int64, secret string) string {
	return hmacB64(fmt.Sprintf("%d\n%s", ts, secret), "")
}

// DingTalkSign 钉钉签名：以 secret 为 key 对 "timestamp(ms)\nsecret" 做 HmacSHA256 后 base64。
func DingTalkSign(tsMillis int64, secret string) string {
	return hmacB64(secret, fmt.Sprintf("%d\n%s", tsMillis, secret))
}

func channels(cfg model.NotifySettings) []notifyChannel {
	var out []notifyChannel
	text := func(title, body string) string { return title + "\n" + body }
	if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
		out = append(out, notifyChannel{"Telegram", func(c *http.Client, t, b string) error {
			return postJSON(c, "https://api.telegram.org/bot"+cfg.TelegramToken+"/sendMessage",
				map[string]any{"chat_id": cfg.TelegramChatID, "text": text(t, b)}, nil)
		}})
	}
	if cfg.BarkURL != "" {
		out = append(out, notifyChannel{"Bark", func(c *http.Client, t, b string) error {
			return postJSON(c, strings.TrimRight(cfg.BarkURL, "/"), map[string]any{"title": t, "body": b, "group": "xray-panel"}, nil)
		}})
	}
	if cfg.FeishuURL != "" {
		out = append(out, notifyChannel{"飞书", func(c *http.Client, t, b string) error {
			payload := map[string]any{"msg_type": "text", "content": map[string]any{"text": text(t, b)}}
			if cfg.FeishuSecret != "" {
				ts := notifyNow().Unix()
				payload["timestamp"] = strconv.FormatInt(ts, 10)
				payload["sign"] = FeishuSign(ts, cfg.FeishuSecret)
			}
			return postJSON(c, cfg.FeishuURL, payload, checkCode("code", "msg"))
		}})
	}
	if cfg.DingTalkURL != "" {
		out = append(out, notifyChannel{"钉钉", func(c *http.Client, t, b string) error {
			u := cfg.DingTalkURL
			if cfg.DingTalkSecret != "" {
				ts := notifyNow().UnixMilli()
				sep := "?"
				if strings.Contains(u, "?") {
					sep = "&"
				}
				u += sep + "timestamp=" + strconv.FormatInt(ts, 10) + "&sign=" + url.QueryEscape(DingTalkSign(ts, cfg.DingTalkSecret))
			}
			return postJSON(c, u, map[string]any{"msgtype": "text", "text": map[string]any{"content": text(t, b)}}, checkCode("errcode", "errmsg"))
		}})
	}
	if cfg.WeComURL != "" {
		out = append(out, notifyChannel{"企业微信", func(c *http.Client, t, b string) error {
			return postJSON(c, cfg.WeComURL, map[string]any{"msgtype": "text", "text": map[string]any{"content": text(t, b)}}, checkCode("errcode", "errmsg"))
		}})
	}
	if cfg.WebhookURL != "" {
		out = append(out, notifyChannel{"Webhook", func(c *http.Client, t, b string) error {
			return postJSON(c, cfg.WebhookURL, map[string]any{"title": t, "body": b, "time": notifyNow().Unix()}, nil)
		}})
	}
	return out
}

// ErrNoChannel 表示没有配置任何通知渠道。
var ErrNoChannel = fmt.Errorf("还没有配置任何通知渠道")

// Notify 向所有已配置的渠道发送消息，返回汇总错误（全部成功返回 nil）。
// client 为 nil 时使用默认客户端（可传入走本地代理的客户端，应对 Telegram 等被墙的情况）。
func Notify(cfg model.NotifySettings, client *http.Client, title, body string) error {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	chs := channels(cfg)
	if len(chs) == 0 {
		return ErrNoChannel
	}
	var errs []string
	for _, ch := range chs {
		if err := ch.send(client, title, body); err != nil {
			errs = append(errs, ch.name+"："+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}

// ValidateNotify 校验通知配置中的地址格式。
func ValidateNotify(cfg model.NotifySettings) error {
	for name, u := range map[string]string{"Bark": cfg.BarkURL, "飞书": cfg.FeishuURL, "钉钉": cfg.DingTalkURL,
		"企业微信": cfg.WeComURL, "Webhook": cfg.WebhookURL} {
		if u == "" {
			continue
		}
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return fmt.Errorf("%s 地址格式错误，应以 http:// 或 https:// 开头", name)
		}
	}
	return nil
}
