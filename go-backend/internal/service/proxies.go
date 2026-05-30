package service

func ProxyToXray(p map[string]any) map[string]any {
	server := map[string]any{"address": p["host"], "port": p["port"]}
	if auth := getAuth(p); auth != nil {
		server["users"] = []any{map[string]any{"user": auth["user"], "pass": auth["pass"]}}
	}
	return map[string]any{
		"tag":      p["tag"],
		"protocol": p["protocol"],
		"settings": map[string]any{"servers": []any{server}},
	}
}
