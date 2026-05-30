package service

var sniffing = map[string]any{
	"enabled":      true,
	"destOverride": []string{"http", "tls", "quic"},
}

func InboundToXray(ib map[string]any) map[string]any {
	settings := map[string]any{}
	auth := getAuth(ib)

	if ib["protocol"] == "socks" {
		if v, ok := ib["udp"]; ok {
			settings["udp"] = v
		} else {
			settings["udp"] = true
		}
		if auth != nil {
			settings["auth"] = "password"
			settings["accounts"] = []any{map[string]any{"user": auth["user"], "pass": auth["pass"]}}
		} else {
			settings["auth"] = "noauth"
		}
	} else {
		if auth != nil {
			settings["accounts"] = []any{map[string]any{"user": auth["user"], "pass": auth["pass"]}}
		}
	}

	return map[string]any{
		"tag":      ib["tag"],
		"listen":   strDefault(ib, "listen", "127.0.0.1"),
		"port":     ib["port"],
		"protocol": ib["protocol"],
		"settings": settings,
		"sniffing": copyMap(sniffing),
	}
}

func getAuth(ib map[string]any) map[string]any {
	a, _ := ib["auth"].(map[string]any)
	if a == nil {
		return nil
	}
	user, _ := a["user"].(string)
	pass, _ := a["pass"].(string)
	if user == "" || pass == "" {
		return nil
	}
	return map[string]any{"user": user, "pass": pass}
}

func strDefault(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
