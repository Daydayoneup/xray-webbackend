package model

// OutboundSpec 是出站的“用户意图”：结构化字段，而不是生成好的 Xray JSON。
// 节点与自定义代理统一使用它持久化，Xray 配置在应用时由 service.BuildOutbound 生成，
// 这样编辑时可以完整回填，Xray 版本差异也只需在生成器一处适配。
type OutboundSpec struct {
	Protocol string `json:"protocol"` // socks http vmess vless trojan shadowsocks
	Host     string `json:"host"`
	Port     int    `json:"port"`

	// socks/http 鉴权
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`

	// vmess/vless: UUID；trojan/shadowsocks: 密码
	UUID string `json:"uuid,omitempty"`
	// shadowsocks: 加密方式；vmess: security（默认 auto）
	Method string `json:"method,omitempty"`
	Flow   string `json:"flow,omitempty"` // vless

	Network    string `json:"network,omitempty"`  // tcp ws grpc h2 httpupgrade xhttp
	Security   string `json:"security,omitempty"` // none tls reality
	SNI        string `json:"sni,omitempty"`
	Path       string `json:"path,omitempty"`        // ws/h2/httpupgrade/xhttp 路径；grpc 为 serviceName
	HostHeader string `json:"host_header,omitempty"` // ws/h2/httpupgrade/xhttp Host
	Mode       string `json:"mode,omitempty"`        // xhttp mode / grpc multi
	ALPN       string `json:"alpn,omitempty"`        // 逗号分隔
	// TLS/Reality
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	ShortID     string `json:"short_id,omitempty"`
	SpiderX     string `json:"spider_x,omitempty"`
}
