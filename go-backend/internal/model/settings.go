package model

// DNSSettings 控制生成配置中的 dns 段。
//   - system：不配置，使用服务器系统 DNS（默认）
//   - split：国内域名用国内 DNS 直连，其余用 DoH（走默认出口），适合服务器在国内
//   - doh：全部用 DoH
//   - custom：自定义服务器列表
type DNSSettings struct {
	Mode          string   `json:"mode"`
	Domestic      []string `json:"domestic,omitempty"`
	Remote        []string `json:"remote,omitempty"`
	Custom        []string `json:"custom,omitempty"`
	QueryStrategy string   `json:"query_strategy,omitempty"` // UseIP | UseIPv4 | UseIPv6
}

type NotifySettings struct {
	TelegramToken  string   `json:"telegram_token,omitempty"`
	TelegramChatID string   `json:"telegram_chat_id,omitempty"`
	BarkURL        string   `json:"bark_url,omitempty"`
	FeishuURL      string   `json:"feishu_url,omitempty"`    // 飞书群机器人 Webhook
	FeishuSecret   string   `json:"feishu_secret,omitempty"` // 飞书“签名校验”密钥（可选）
	DingTalkURL    string   `json:"dingtalk_url,omitempty"`  // 钉钉群机器人 Webhook
	DingTalkSecret string   `json:"dingtalk_secret,omitempty"`
	WeComURL       string   `json:"wecom_url,omitempty"` // 企业微信群机器人 Webhook
	WebhookURL     string   `json:"webhook_url,omitempty"`
	Events         []string `json:"events"` // 启用的事件，见 app.NotifyEvents
}

type GeoSettings struct {
	Source         string `json:"source"`           // loyalsoldier | v2fly
	AutoUpdateDays int    `json:"auto_update_days"` // 0 = 关闭
	UpdatedAt      int64  `json:"updated_at"`
	LastError      string `json:"last_error,omitempty"`
}
