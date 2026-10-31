package service

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"xray-panel/internal/model"
)

var userNameRe = regexp.MustCompile(`^[\p{L}\p{N}_.\-]{1,32}$`)

// ValidateUserName 校验用户名：它同时是 socks/http 的账号和 Xray 的 email。
func ValidateUserName(name string) error {
	if !userNameRe.MatchString(name) {
		return fmt.Errorf("用户名只能包含字母、数字、中文和 _ . -，最长 32 个字符")
	}
	return nil
}

// ValidUserSSKey 校验用户的 ss2022 主密钥（base64，16 或 32 字节）。
func ValidUserSSKey(key string) bool {
	raw, err := base64.StdEncoding.DecodeString(key)
	return err == nil && (len(raw) == 16 || len(raw) == 32)
}

// SSUserKey 由用户主密钥派生出指定加密方式所需长度的密钥：够长取前缀，不够（旧数据的 16 字节密钥
// 用在 32 字节加密方式上）则用 SHA-256 扩展。配置与分享链接都用这个函数，保证一致。
func SSUserKey(master, method string) string {
	n := SSKeyLen(method)
	raw, err := base64.StdEncoding.DecodeString(master)
	if err != nil || n == 0 {
		return master
	}
	if len(raw) < n {
		sum := sha256.Sum256(raw)
		raw = sum[:]
	}
	return base64.StdEncoding.EncodeToString(raw[:n])
}

// UserSecret 返回用户在某个协议下的凭据。
func UserSecret(u *model.User, ib *model.Inbound) string {
	switch ib.Protocol {
	case "vless":
		return u.UUID
	case "shadowsocks":
		if u.SSKey == "" {
			return ""
		}
		return SSUserKey(u.SSKey, ib.SSMethod)
	default:
		return u.Password
	}
}

// InboundUsers 返回关联到该入站的用户（含已停用的）。旧数据（入站中直接保存用户）原样返回。
func InboundUsers(state *model.PanelState, ib *model.Inbound) []model.InboundUser {
	if len(ib.Users) > 0 {
		return ib.Users
	}
	var out []model.InboundUser
	for i := range state.Users {
		u := &state.Users[i]
		if containsStr(u.Inbounds, ib.Tag) {
			out = append(out, model.InboundUser{Name: u.Name, Secret: UserSecret(u, ib), Disabled: u.Disabled})
		}
	}
	return out
}

// NewUserCredentials 为缺失的凭据生成随机值。
func NewUserCredentials(u *model.User) {
	if u.UUID == "" {
		u.UUID = NewUUID()
	}
	if u.Password == "" {
		u.Password = strings.ReplaceAll(NewUUID(), "-", "")[:16]
	}
	if u.SSKey == "" {
		u.SSKey = NewSSKey("2022-blake3-aes-256-gcm")
	}
}
