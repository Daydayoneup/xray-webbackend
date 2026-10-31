package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// nextTag 按序号生成 tag，并跳过已被占用的（防止数据中序号与已有 tag 不一致时产生重复）。
func nextTag(prefix string, seq *int, taken func(string) bool) string {
	for {
		tag := fmt.Sprintf("%s%d", prefix, *seq)
		*seq++
		if !taken(tag) {
			return tag
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, detail string) {
	writeJSON(w, code, map[string]string{"detail": detail})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

var fieldNames = map[string]string{
	"Protocol": "协议", "Listen": "监听地址", "Port": "端口", "User": "账号", "Password": "密码",
	"NewPassword": "新密码", "OldPassword": "原密码", "Nodes": "成员节点", "Strategy": "策略",
	"Type": "匹配类型", "Outbound": "出口", "URL": "订阅链接", "DomainStrategy": "域名策略",
	"AutoUpdateHours": "自动更新间隔", "Tags": "节点", "Mode": "测速方式",
}

func translateValidation(err error) string {
	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		return err.Error()
	}
	var msgs []string
	for _, e := range errs {
		name := fieldNames[e.Field()]
		if name == "" {
			name = e.Field()
		}
		var m string
		switch e.Tag() {
		case "required", "required_with":
			m = name + "不能为空"
		case "min":
			if e.Kind().String() == "slice" {
				m = fmt.Sprintf("%s至少选择 %s 个", name, e.Param())
			} else if e.Kind().String() == "string" {
				m = fmt.Sprintf("%s至少 %s 个字符", name, e.Param())
			} else {
				m = fmt.Sprintf("%s不能小于 %s", name, e.Param())
			}
		case "max":
			m = fmt.Sprintf("%s不能大于 %s", name, e.Param())
		case "oneof":
			m = fmt.Sprintf("%s只能是 %s 之一", name, strings.ReplaceAll(e.Param(), " ", " / "))
		case "ip":
			m = name + "必须是 IP 地址，例如 127.0.0.1 或 0.0.0.0"
		default:
			m = fmt.Sprintf("%s格式错误", name)
		}
		msgs = append(msgs, m)
	}
	return strings.Join(msgs, "；")
}
