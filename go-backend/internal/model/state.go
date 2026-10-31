package model

import "encoding/json"

// Auth — username/password auth. Serializes password as "pass" in JSON.
type Auth struct {
	User     string `json:"user"`
	Password string `json:"-"`
}

func (a Auth) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}{a.User, a.Password})
}

func (a *Auth) UnmarshalJSON(b []byte) error {
	var raw struct {
		User     string `json:"user"`
		Pass     string `json:"pass"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.User = raw.User
	a.Password = raw.Pass
	if a.Password == "" {
		a.Password = raw.Password // populate_by_name compat
	}
	return nil
}

type PasswordRec struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

// SubUsage — 来自订阅响应头 subscription-userinfo 的流量/到期信息（字节 / Unix 秒）。
type SubUsage struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}

type Subscription struct {
	ID        int       `json:"id"`
	URL       string    `json:"url"`
	Name      string    `json:"name"`    // 用户自定义名称
	Remarks   string    `json:"remarks"` // 订阅内容里的 REMARKS
	Status    string    `json:"status"`
	FetchedAt int64     `json:"fetched_at"` // 最近一次成功拉取
	TriedAt   int64     `json:"tried_at"`   // 最近一次尝试拉取（含失败）
	LastError string    `json:"last_error"`
	Usage     *SubUsage `json:"usage,omitempty"`
	// 自动更新间隔（小时），0 = 关闭
	AutoUpdateHours int `json:"auto_update_hours"`
	// Exclude 排除关键字，"|" 分隔，节点名包含任一关键字（不区分大小写）即跳过
	Exclude string `json:"exclude"`
	// NamePrefix 加在节点名前，便于区分多个订阅
	NamePrefix string `json:"name_prefix"`
	// Filtered 最近一次拉取被排除的节点数
	Filtered int `json:"filtered"`
}

// LegacySubID 标记旧版本迁移来、尚不知道归属哪个订阅的节点。
const LegacySubID = -1

type Node struct {
	Tag         string        `json:"tag"` // 稳定 ID：刷新订阅后保持不变
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Host        string        `json:"host"`
	Port        int           `json:"port"`
	SubID       int           `json:"sub_id"`
	Disabled    bool          `json:"disabled,omitempty"`
	Missing     bool          `json:"missing,omitempty"` // 订阅里已不存在，但仍被规则/节点组引用
	Latency     *int          `json:"latency,omitempty"`
	LatencyMode string        `json:"latency_mode,omitempty"` // real | tcp
	Spec        *OutboundSpec `json:"spec,omitempty"`
	// Outbound 仅用于兼容旧数据：无法还原成 Spec 的旧节点保留原始出站 JSON
	Outbound map[string]any `json:"outbound,omitempty"`
}

// User 是面板用户：一个用户可以关联多个入站，在各协议中使用对应的凭据。
// Name 同时是 Xray 中的 email（流量统计、在线识别），全局唯一。
type User struct {
	Name      string   `json:"name"`
	Remark    string   `json:"remark,omitempty"`
	Disabled  bool     `json:"disabled,omitempty"`
	UUID      string   `json:"uuid"`     // vless
	Password  string   `json:"password"` // socks / http
	SSKey     string   `json:"ss_key"`   // shadowsocks 2022（base64，16 字节加密方式取前 16 字节）
	Inbounds  []string `json:"inbounds"` // 关联的入站 tag
	CreatedAt int64    `json:"created_at,omitempty"`
}

// InboundDNS 是入站独立 DNS：经该入站发出的 DNS 查询（目标端口 53）改发到 Server。
type InboundDNS struct {
	Server string `json:"server"`        // IP 或 IP:端口
	Via    string `json:"via,omitempty"` // 出口：空 = 直连；节点或自定义代理 tag
}

// InboundUser 是生成配置时某个入站的有效用户（由 User 解析而来）。socks/http: Secret=密码；
// vless: Secret=UUID；shadowsocks(2022): Secret=用户密钥(base64)。v6 之前的数据也以这种形式直接存在入站中。
type InboundUser struct {
	Name     string `json:"name"`
	Secret   string `json:"secret"`
	Disabled bool   `json:"disabled,omitempty"`
}

// RealityConf 是 vless+reality 入站的参数（无需证书）。
type RealityConf struct {
	Dest        string   `json:"dest"`         // 伪装目标，如 www.microsoft.com:443
	ServerNames []string `json:"server_names"` // 客户端可用的 SNI
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortIDs    []string `json:"short_ids"`
}

type Inbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"` // socks | http | vless | shadowsocks
	Remark   string `json:"remark,omitempty"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	UDP      bool   `json:"udp"`
	// Users 仅用于兼容 v6 之前的数据（迁移到 PanelState.Users）；生成配置时由关联用户解析填充
	Users    []InboundUser `json:"users,omitempty"`
	DNS      *InboundDNS   `json:"dns,omitempty"`
	Reality  *RealityConf  `json:"reality,omitempty"`
	SSMethod string        `json:"ss_method,omitempty"`
	SSKey    string        `json:"ss_key,omitempty"` // ss2022 服务端密钥
	// Auth 仅用于兼容旧数据（v4 起迁移到 Users）
	Auth *Auth `json:"auth,omitempty"`
}

type Proxy struct {
	Tag      string        `json:"tag"`
	Name     string        `json:"name"`
	Protocol string        `json:"protocol"`
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Auth     *Auth         `json:"auth,omitempty"` // 旧数据兼容，新数据写入 Spec.User/Pass
	Link     string        `json:"link,omitempty"`
	Via      string        `json:"via,omitempty"` // 前置出站 tag（链式代理 dialerProxy）
	Spec     *OutboundSpec `json:"spec,omitempty"`
	// RawOutbound 仅用于兼容旧数据
	RawOutbound map[string]any `json:"rawOutbound,omitempty"`
}

type Balancer struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	// Nodes 手动选择的固定成员（节点或自定义代理）
	Nodes []string `json:"nodes"`
	// Filter 按名称关键字自动收纳节点（"|" 分隔，任一命中即加入），刷新订阅后自动包含新节点
	Filter string `json:"filter,omitempty"`
	// FilterSubID 限定自动收纳的订阅，nil 表示全部订阅
	FilterSubID *int `json:"filter_sub_id,omitempty"`
	// Strategy: leastPing | random | roundRobin | select(手动选择，使用 Selected)
	Strategy string `json:"strategy"`
	Selected string `json:"selected,omitempty"`
}

// RuleSet 是自定义的域名 / IP 分组，分流规则通过 type=set、value=Tag 引用。
// Domains：example.com（后缀）或 full: / keyword: / regexp: / geosite: 前缀；IPs：IP、CIDR 或 geoip:xx。
type RuleSet struct {
	Tag     string   `json:"tag"`
	Name    string   `json:"name"`
	Remark  string   `json:"remark,omitempty"`
	Domains []string `json:"domains"`
	IPs     []string `json:"ips"`
	// Source 非空表示内容来自远程列表（如 GFWList），Domains/IPs 由更新时解析生成
	Source *RuleSetSource `json:"source,omitempty"`
}

// RuleSetSource 是分组的订阅来源。
type RuleSetSource struct {
	URL         string `json:"url"`
	Format      string `json:"format"`       // auto | gfwlist | clash | domains
	UpdateHours int    `json:"update_hours"` // 0 = 只手动更新
	UpdatedAt   int64  `json:"updated_at,omitempty"`
	TriedAt     int64  `json:"tried_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	Detected    string `json:"detected,omitempty"` // 实际识别出的格式
	Skipped     int    `json:"skipped,omitempty"`  // 无法表达而跳过的条目（白名单、正则、通配符）
}

type Rule struct {
	ID       int    `json:"id"`
	Name     string `json:"name,omitempty"` // 可选的规则名称，显示在规则测试与生效路由中
	Type     string `json:"type"`
	Value    string `json:"value"`
	Outbound string `json:"outbound"`
	Enabled  bool   `json:"enabled"`
}

type PanelState struct {
	SchemaVersion   int            `json:"schema_version"`
	Password        *PasswordRec   `json:"password,omitempty"`
	Subscriptions   []Subscription `json:"subscriptions"`
	Nodes           []Node         `json:"nodes"`
	Inbounds        []Inbound      `json:"inbounds"`
	Users           []User         `json:"users"`
	RuleSets        []RuleSet      `json:"rule_sets"`
	RuleSetSeq      int            `json:"rule_set_seq"`
	Proxies         []Proxy        `json:"proxies"`
	Balancers       []Balancer     `json:"balancers"`
	Rules           []Rule         `json:"rules"`
	DefaultOutbound string         `json:"default_outbound"`
	DomainStrategy  string         `json:"domain_strategy"`
	DNS             DNSSettings    `json:"dns"`
	Notify          NotifySettings `json:"notify"`
	Geo             GeoSettings    `json:"geo"`
	PublicHost      string         `json:"public_host"` // 生成分享链接用的服务器地址
	InboundSeq      int            `json:"inbound_seq"`
	ProxySeq        int            `json:"proxy_seq"`
	BalancerSeq     int            `json:"balancer_seq"`
	SubSeq          int            `json:"sub_seq"`
}
