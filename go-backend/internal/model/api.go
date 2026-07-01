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

type InboundIn struct {
	Protocol string  `json:"protocol" validate:"required,oneof=socks http"`
	Listen   string  `json:"listen"`
	Port     int     `json:"port" validate:"required,min=1,max=65535"`
	UDP      bool    `json:"udp"`
	Auth     *AuthIn `json:"auth" validate:"omitempty"`
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
	// manual mode — vmess/vless/trojan/ss
	UUID        string `json:"uuid"`
	Method      string `json:"method"`
	Network     string `json:"network"`
	TLS         string `json:"tls"`
	SNI         string `json:"sni"`
	Path        string `json:"path"`
	WsHost      string `json:"ws_host"`
	Flow        string `json:"flow"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	ShortId     string `json:"short_id"`
	SpiderX     string `json:"spider_x"`
}

type BalancerIn struct {
	Name  string   `json:"name"`
	Nodes []string `json:"nodes" validate:"required,min=1"`
}

type RuleIn struct {
	ID       *int   `json:"id"`
	Type     string `json:"type" validate:"required,oneof=domain-suffix full keyword geosite ip geoip port"`
	Value    string `json:"value"`
	Outbound string `json:"outbound" validate:"required"`
	Enabled  bool   `json:"enabled"`
}

type RoutingIn struct {
	DefaultOutbound string   `json:"default_outbound"`
	Rules           []RuleIn `json:"rules"`
}

type SubscriptionIn struct {
	URL string `json:"url" validate:"required"`
}
