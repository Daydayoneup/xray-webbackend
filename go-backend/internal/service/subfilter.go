package service

import "strings"

// DefaultExclude 过滤机场塞进订阅的“信息节点”（剩余流量、到期时间、官网地址等）。
const DefaultExclude = "剩余|到期|过期|官网|网址|套餐|重置|expire"

// Keywords 把 "a|b, c" 拆成关键字列表（支持 | 和 , 分隔）。
func Keywords(s string) []string {
	var out []string
	for _, k := range strings.FieldsFunc(s, func(r rune) bool { return r == '|' || r == ',' || r == '，' || r == '\n' }) {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, strings.ToLower(k))
		}
	}
	return out
}

// FilterNodes 按排除关键字过滤节点（匹配原始节点名，不区分大小写），再加上名称前缀。
// 返回保留的节点和被排除的节点名。
func FilterNodes(nodes []NodeRaw, exclude, prefix string) ([]NodeRaw, []string) {
	kws := Keywords(exclude)
	var kept []NodeRaw
	var dropped []string
	for _, n := range nodes {
		name := strings.ToLower(n.Name)
		hit := false
		for _, k := range kws {
			if strings.Contains(name, k) {
				hit = true
				break
			}
		}
		if hit {
			dropped = append(dropped, n.Name)
			continue
		}
		if prefix != "" {
			n.Name = prefix + n.Name
		}
		kept = append(kept, n)
	}
	return kept, dropped
}
