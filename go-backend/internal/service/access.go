package service

import (
	"net"
	"regexp"
	"strings"
)

// AccessEvent 是 Xray 访问日志中的一条连接记录。
//
// Xray 默认把访问日志写到标准输出，格式形如：
//
//	2025/01/02 15:04:05.123456 from 1.2.3.4:51234 accepted tcp:www.google.com:443 [in-0 >> node-1] email: alice@in-2
//
// 旧版本没有 "from " 前缀、来源地址可能带 "tcp:" 前缀；路由信息中的分隔符为 ">>"（规则命中）或 "->"（默认出口）。
type AccessEvent struct {
	IP       string
	Port     string
	Network  string // tcp | udp（目标的网络类型）
	Dest     string // 目标地址 host:port
	Inbound  string
	Outbound string
	Email    string
}

var accessRe = regexp.MustCompile(`^(?:\d{4}/\d\d/\d\d \d\d:\d\d:\d\d(?:\.\d+)? )?(?:from )?(?:(?:tcp|udp):)?(\S+) accepted (?:(tcp|udp):)?(\S+)(?: \[([^\]]*)\])?(.*)$`)

// ParseAccessLine 解析一行访问日志，非访问日志（错误日志等）返回 false。
func ParseAccessLine(line string) (AccessEvent, bool) {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, " accepted ") {
		return AccessEvent{}, false
	}
	m := accessRe.FindStringSubmatch(line)
	if m == nil {
		return AccessEvent{}, false
	}
	host, port, err := net.SplitHostPort(m[1])
	if err != nil {
		return AccessEvent{}, false
	}
	ev := AccessEvent{IP: host, Port: port, Network: m[2], Dest: m[3]}
	if ev.Network == "" {
		ev.Network = "tcp"
	}
	if d := m[4]; d != "" {
		sep := " >> "
		if !strings.Contains(d, sep) {
			sep = " -> "
		}
		in, out, _ := strings.Cut(d, sep)
		ev.Inbound, ev.Outbound = strings.TrimSpace(in), strings.TrimSpace(out)
	}
	if i := strings.Index(m[5], "email: "); i >= 0 {
		ev.Email = strings.TrimSpace(m[5][i+len("email: "):])
	}
	return ev, true
}
