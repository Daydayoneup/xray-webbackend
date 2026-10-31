package model

type LoginIn struct {
	Password string `json:"password" validate:"required"`
}

type AuthIn struct {
	User     string `json:"user" validate:"required_with=Password"`
	Password string `json:"pass" validate:"required_with=User"`
}

type PasswordChangeIn struct {
	OldPassword string `json:"old_password" validate:"required"`
	NewPassword string `json:"new_password" validate:"required,min=6"`
}

type UserIn struct {
	Name     string   `json:"name" validate:"max=32"`
	Remark   string   `json:"remark" validate:"max=40"`
	Disabled bool     `json:"disabled"`
	UUID     string   `json:"uuid" validate:"max=64"`
	Password string   `json:"password" validate:"max=64"`
	SSKey    string   `json:"ss_key" validate:"max=128"`
	Inbounds []string `json:"inbounds"`
}

type InboundDNSIn struct {
	Server string `json:"server" validate:"max=64"`
	Via    string `json:"via"`
}

type InboundIn struct {
	Protocol string        `json:"protocol" validate:"required,oneof=socks http vless shadowsocks"`
	Remark   string        `json:"remark" validate:"max=40"`
	Listen   string        `json:"listen" validate:"omitempty,ip"`
	Port     int           `json:"port" validate:"required,min=1,max=65535"`
	UDP      bool          `json:"udp"`
	Users    []string      `json:"users"` // 关联的用户名
	DNS      *InboundDNSIn `json:"dns"`
	Auth     *AuthIn       `json:"auth" validate:"omitempty"` // 兼容旧客户端：自动建同名用户
	// vless + reality
	RealityDest        string   `json:"reality_dest"`
	RealityServerNames []string `json:"reality_server_names"`
	RealityPrivateKey  string   `json:"reality_private_key"`
	RealityShortIDs    []string `json:"reality_short_ids"`
	// shadowsocks 2022
	SSMethod string `json:"ss_method"`
	SSKey    string `json:"ss_key"`
}

type ProxyIn struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol" validate:"required,oneof=socks http vmess vless trojan shadowsocks"`
	// socks/http manual fields
	Host string  `json:"host"`
	Port int     `json:"port" validate:"omitempty,min=1,max=65535"`
	Auth *AuthIn `json:"auth" validate:"omitempty"`
	// link paste
	Link string `json:"link"`
	// 链式代理：经由该出口连接本代理
	Via string `json:"via"`
	// manual mode — vmess/vless/trojan/ss
	UUID        string `json:"uuid"`
	Method      string `json:"method"`
	Network     string `json:"network"`
	TLS         string `json:"tls"`
	SNI         string `json:"sni"`
	Path        string `json:"path"`
	WsHost      string `json:"ws_host"`
	Mode        string `json:"mode"`
	ALPN        string `json:"alpn"`
	Flow        string `json:"flow"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	ShortId     string `json:"short_id"`
	SpiderX     string `json:"spider_x"`
}

type BalancerIn struct {
	Name        string   `json:"name"`
	Nodes       []string `json:"nodes"`
	Filter      string   `json:"filter" validate:"max=200"`
	FilterSubID *int     `json:"filter_sub_id"`
	Strategy    string   `json:"strategy" validate:"omitempty,oneof=leastPing random roundRobin select"`
	Selected    string   `json:"selected"`
}

type RuleIn struct {
	ID       *int   `json:"id"`
	Name     string `json:"name" validate:"max=40"`
	Type     string `json:"type" validate:"required,oneof=domain-suffix full keyword geosite ip geoip port set"`
	Value    string `json:"value"`
	Outbound string `json:"outbound" validate:"required"`
	Enabled  bool   `json:"enabled"`
}

type RuleSetIn struct {
	Name    string           `json:"name" validate:"required,max=40"`
	Remark  string           `json:"remark" validate:"max=80"`
	Domains []string         `json:"domains" validate:"max=5000"`
	IPs     []string         `json:"ips" validate:"max=5000"`
	Source  *RuleSetSourceIn `json:"source"` // 非空且 URL 非空 = 订阅远程列表
}

type RuleSetSourceIn struct {
	URL         string `json:"url" validate:"max=1000"`
	Format      string `json:"format" validate:"omitempty,oneof=auto gfwlist clash domains"`
	UpdateHours int    `json:"update_hours" validate:"min=0,max=720"`
}

type RoutingIn struct {
	DefaultOutbound string   `json:"default_outbound"`
	DomainStrategy  string   `json:"domain_strategy" validate:"omitempty,oneof=AsIs IPIfNonMatch IPOnDemand"`
	Rules           []RuleIn `json:"rules" validate:"dive"`
}

type SubscriptionIn struct {
	URL             string  `json:"url" validate:"required"`
	Name            string  `json:"name"`
	AutoUpdateHours int     `json:"auto_update_hours" validate:"min=0,max=720"`
	Exclude         *string `json:"exclude" validate:"omitempty,max=500"` // nil = 使用默认关键字
	NamePrefix      string  `json:"name_prefix" validate:"max=20"`
}

type SubscriptionUpdateIn struct {
	URL             *string `json:"url"`
	Name            *string `json:"name"`
	AutoUpdateHours *int    `json:"auto_update_hours" validate:"omitempty,min=0,max=720"`
	Exclude         *string `json:"exclude" validate:"omitempty,max=500"`
	NamePrefix      *string `json:"name_prefix" validate:"omitempty,max=20"`
}

type NodeUpdateIn struct {
	Disabled *bool `json:"disabled"`
}

type NodeBatchIn struct {
	Tags     []string `json:"tags" validate:"required,min=1"`
	Disabled bool     `json:"disabled"`
}

type TestNodesIn struct {
	Mode string   `json:"mode" validate:"omitempty,oneof=real tcp"`
	Tags []string `json:"tags"` // 为空表示全部
}
