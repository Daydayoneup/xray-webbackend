# Proxy Form Manual + Link Dual Mode — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** All 6 protocols support both manual fill and link paste via Tab switching, with shared backend Build*Outbound functions.

**Architecture:** Backend: extend ProxyIn with 12 manual-mode fields → add StreamOpts + Build*Outbound to service/proxies.go → refactor xray_sub.go parsers to delegate to Build*Outbound → update buildProxy to route by link presence. Frontend: add mode state + el-tabs + manual form panel with collapsible advanced section + field visibility toggles.

**Tech Stack:** Go 1.25, chi v5, Vue 3 + Element Plus

---

### Task 1: Extend ProxyIn model with manual-mode fields

**Files:**
- Modify: `go-backend/internal/model/api.go:25-32`

- [ ] **Step 1: Add 12 new optional fields to ProxyIn struct**

Replace the existing `ProxyIn` struct:

```go
type ProxyIn struct {
	Name     string  `json:"name"`
	Protocol string  `json:"protocol" validate:"required,oneof=socks http vmess vless trojan shadowsocks"`
	// socks/http manual fields
	Host     string  `json:"host"`
	Port     int     `json:"port" validate:"omitempty,min=1,max=65535"`
	Auth     *AuthIn `json:"auth" validate:"omitempty"`
	// link paste
	Link     string  `json:"link"`
	// manual mode — vmess/vless/trojan/ss
	UUID          string `json:"uuid"`
	Method        string `json:"method"`
	Network       string `json:"network"`
	TLS           string `json:"tls"`
	SNI           string `json:"sni"`
	Path          string `json:"path"`
	WsHost        string `json:"ws_host"`
	Flow          string `json:"flow"`
	Fingerprint   string `json:"fingerprint"`
	PublicKey     string `json:"public_key"`
	ShortId       string `json:"short_id"`
	SpiderX       string `json:"spider_x"`
	AllowInsecure bool   `json:"allow_insecure"`
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd go-backend && go build ./...
```

Expected: PASS (model package compiles; handler/proxies.go won't break since new fields are just additions)

- [ ] **Step 3: Commit**

```bash
git add go-backend/internal/model/api.go
git commit -m "feat: extend ProxyIn with manual-mode fields for all protocols"
```

---

### Task 2: Add model validation tests for manual fields

**Files:**
- Modify: `go-backend/internal/model/api_test.go:48-59`

- [ ] **Step 1: Replace TestProxyInValidation with expanded test cases**

Replace the existing `TestProxyInValidation` function:

```go
func TestProxyInValidation(t *testing.T) {
	// socks/http: port 0 is ok since host:port validation moved to handler
	valid := ProxyIn{Protocol: "socks", Host: "10.0.0.1", Port: 1080}
	if err := validate.Struct(valid); err != nil {
		t.Errorf("valid proxy should pass: %v", err)
	}
	// vmess without link: allowed (handler will reject if no link)
	vmess := ProxyIn{Protocol: "vmess", Link: "vmess://test"}
	if err := validate.Struct(vmess); err != nil {
		t.Errorf("vmess with link should pass: %v", err)
	}
	// manual vmess with host/port/uuid should pass
	manualVMess := ProxyIn{Protocol: "vmess", Host: "1.2.3.4", Port: 443, UUID: "test-uuid"}
	if err := validate.Struct(manualVMess); err != nil {
		t.Errorf("manual vmess should pass: %v", err)
	}
	// manual trojan with host/port/uuid should pass
	manualTrojan := ProxyIn{Protocol: "trojan", Host: "1.2.3.4", Port: 443, UUID: "password"}
	if err := validate.Struct(manualTrojan); err != nil {
		t.Errorf("manual trojan should pass: %v", err)
	}
	// manual ss with host/port/method should pass
	manualSS := ProxyIn{Protocol: "shadowsocks", Host: "1.2.3.4", Port: 8388, Method: "aes-256-gcm", UUID: "password"}
	if err := validate.Struct(manualSS); err != nil {
		t.Errorf("manual ss should pass: %v", err)
	}
	// manual vmess with advanced fields
	manualAdv := ProxyIn{
		Protocol: "vmess", Host: "1.2.3.4", Port: 443, UUID: "uuid",
		Network: "ws", TLS: "tls", SNI: "example.com", Path: "/ws", WsHost: "example.com",
		Fingerprint: "chrome",
	}
	if err := validate.Struct(manualAdv); err != nil {
		t.Errorf("manual vmess with advanced should pass: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

```bash
cd go-backend && go test ./internal/model/ -v -run TestProxyInValidation
```

Expected: PASS (6/6 subtests)

- [ ] **Step 3: Commit**

```bash
git add go-backend/internal/model/api_test.go
git commit -m "test: add manual-mode validation test cases for ProxyIn"
```

---

### Task 3: Add StreamOpts and Build*Outbound functions to service/proxies.go

**Files:**
- Modify: `go-backend/internal/service/proxies.go`

- [ ] **Step 1: Replace the entire file with StreamOpts type + Build*Outbound functions**

Replace the content of `service/proxies.go`:

```go
package service

// StreamOpts holds transport/security parameters shared by all outbound builders.
type StreamOpts struct {
	Network       string // tcp, ws, grpc, h2
	Security      string // none, tls, reality
	SNI           string
	Path          string
	Host          string
	Fingerprint   string
	PublicKey     string
	ShortId       string
	SpiderX       string
	AllowInsecure bool
}

func BuildStreamSettings(opts StreamOpts) map[string]any {
	stream := map[string]any{"network": selStr(opts.Network, "tcp")}
	switch opts.Security {
	case "reality":
		stream["security"] = "reality"
		rs := map[string]any{}
		if opts.SNI != "" {
			rs["serverName"] = opts.SNI
		}
		if opts.Fingerprint != "" {
			rs["fingerprint"] = opts.Fingerprint
		}
		if opts.PublicKey != "" {
			rs["publicKey"] = opts.PublicKey
		}
		if opts.ShortId != "" {
			rs["shortId"] = opts.ShortId
		}
		if opts.SpiderX != "" {
			rs["spiderX"] = opts.SpiderX
		}
		if len(rs) > 0 {
			stream["realitySettings"] = rs
		}
	case "tls":
		stream["security"] = "tls"
		ts := map[string]any{"allowInsecure": opts.AllowInsecure}
		sni := opts.SNI
		if sni == "" {
			sni = opts.Host
		}
		if sni != "" {
			ts["serverName"] = sni
		}
		stream["tlsSettings"] = ts
	default:
		stream["security"] = "none"
	}
	net := opts.Network
	if net == "ws" {
		path := opts.Path
		if path == "" {
			path = "/"
		}
		stream["wsSettings"] = map[string]any{"path": path, "headers": map[string]any{"Host": opts.Host}}
	} else if net == "grpc" {
		svc := opts.Path
		if svc == "" {
			svc = opts.Host
		}
		stream["grpcSettings"] = map[string]any{"serviceName": svc}
	}
	return stream
}

func BuildVMessOutbound(host string, port int, uuid string, stream StreamOpts) map[string]any {
	return map[string]any{
		"protocol": "vmess",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": host, "port": port,
					"users": []any{
						map[string]any{
							"id": uuid, "alterId": 0, "security": "auto",
						},
					},
				},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildVLessOutbound(host string, port int, uuid string, flow string, stream StreamOpts) map[string]any {
	user := map[string]any{"id": uuid, "encryption": "none"}
	if flow != "" {
		user["flow"] = flow
	}
	return map[string]any{
		"protocol": "vless",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{"address": host, "port": port, "users": []any{user}},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildTrojanOutbound(host string, port int, password string, stream StreamOpts) map[string]any {
	return map[string]any{
		"protocol": "trojan",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{"address": host, "port": port, "password": password},
			},
		},
		"streamSettings": BuildStreamSettings(stream),
	}
}

func BuildSSOutbound(host string, port int, method string, password string) map[string]any {
	return map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{"address": host, "port": port, "method": method, "password": password},
			},
		},
	}
}

// ProxyToXray builds a socks/http outbound. Kept for backward compatibility.
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
```

- [ ] **Step 2: Verify compilation**

```bash
cd go-backend && go build ./...
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add go-backend/internal/service/proxies.go
git commit -m "feat: add Build*Outbound functions and StreamOpts for manual proxy creation"
```

---

### Task 4: Refactor xray_sub.go parsers to delegate to Build*Outbound

**Files:**
- Modify: `go-backend/internal/service/xray_sub.go:116-355`

- [ ] **Step 1: Refactor parseVMess (lines 116-163)**

Replace the `parseVMess` function body to use `BuildVMessOutbound`:

```go
func parseVMess(link string) (NodeRaw, error) {
	b64 := link[len("vmess://"):]
	raw := b64decode(b64)
	var v vmessConfig
	if err := json.Unmarshal(raw, &v); err != nil {
		return NodeRaw{}, fmt.Errorf("vmess JSON解析失败: %w", err)
	}
	port := toInt(v.Port)
	net := v.Net
	if net == "" {
		net = "tcp"
	}
	tls := strings.ToLower(v.TLS)
	sni := v.SNI
	if sni == "" {
		sni = v.Host
	}
	if sni == "" {
		sni = v.Add
	}
	path := v.Path
	if path == "" {
		path = "/"
	}
	outbound := BuildVMessOutbound(v.Add, port, v.ID, StreamOpts{
		Network:  net,
		Security: tls,
		SNI:      sni,
		Path:     path,
		Host:     v.Host,
	})
	name := v.PS
	if name == "" {
		name = v.Add
	}
	return NodeRaw{Name: name, Type: "vmess", Host: v.Add, Port: port, Outbound: outbound}, nil
}
```

- [ ] **Step 2: Refactor parseVLess (lines 185-249)**

Replace with:

```go
func parseVLess(link string) (NodeRaw, error) {
	u, err := url.Parse(link)
	if err != nil {
		return NodeRaw{}, err
	}
	q := u.Query()
	host := u.Hostname()
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	sni := q.Get("sni")
	if sni == "" && security != "none" {
		sni = host
	}
	outbound := BuildVLessOutbound(host, port, u.User.Username(), q.Get("flow"), StreamOpts{
		Network:       net,
		Security:      security,
		SNI:           sni,
		Path:          q.Get("path"),
		Host:          q.Get("host"),
		Fingerprint:   q.Get("fp"),
		PublicKey:     q.Get("pbk"),
		ShortId:       q.Get("sid"),
		SpiderX:       q.Get("spx"),
		AllowInsecure: q.Get("allowInsecure") == "1",
	})
	name := u.Fragment
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "vless", Host: host, Port: port, Outbound: outbound}, nil
}
```

- [ ] **Step 3: Refactor parseTrojan (lines 253-298)**

Replace with:

```go
func parseTrojan(link string) (NodeRaw, error) {
	u, err := url.Parse(link)
	if err != nil {
		return NodeRaw{}, err
	}
	q := u.Query()
	host := u.Hostname()
	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		sni = host
	}
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	outbound := BuildTrojanOutbound(host, port, u.User.Username(), StreamOpts{
		Network:       net,
		Security:      "tls",
		SNI:           sni,
		Path:          q.Get("path"),
		Host:          q.Get("host"),
		AllowInsecure: q.Get("allowInsecure") == "1",
	})
	name := u.Fragment
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "trojan", Host: host, Port: port, Outbound: outbound}, nil
}
```

- [ ] **Step 4: Refactor parseSS (lines 300-355)**

Replace with:

```go
func parseSS(link string) (NodeRaw, error) {
	body := link[len("ss://"):]
	frag := ""
	if idx := strings.Index(body, "#"); idx >= 0 {
		body, frag = body[:idx], body[idx+1:]
	}
	if idx := strings.Index(body, "?"); idx >= 0 {
		body = body[:idx]
	}
	var method, password, server string
	if idx := strings.LastIndex(body, "@"); idx >= 0 {
		userinfo := body[:idx]
		server = body[idx+1:]
		dec := b64decode(userinfo)
		parts := strings.SplitN(string(dec), ":", 2)
		if len(parts) == 2 {
			method, password = parts[0], parts[1]
		} else {
			parts = strings.SplitN(userinfo, ":", 2)
			if len(parts) == 2 {
				method, password = parts[0], parts[1]
			}
		}
	} else {
		dec := b64decode(body)
		parts := strings.SplitN(string(dec), "@", 2)
		if len(parts) != 2 {
			return NodeRaw{}, fmt.Errorf("ss格式错误")
		}
		uparts := strings.SplitN(parts[0], ":", 2)
		if len(uparts) != 2 {
			return NodeRaw{}, fmt.Errorf("ss userinfo格式错误")
		}
		method, password, server = uparts[0], uparts[1], parts[1]
	}
	hostPort := strings.Split(server, ":")
	if len(hostPort) != 2 {
		return NodeRaw{}, fmt.Errorf("ss server格式错误")
	}
	host := hostPort[0]
	port := toInt(hostPort[1])
	outbound := BuildSSOutbound(host, port, method, password)
	name, _ := url.QueryUnescape(frag)
	if name == "" {
		name = host
	}
	return NodeRaw{Name: name, Type: "shadowsocks", Host: host, Port: port, Outbound: outbound}, nil
}
```

- [ ] **Step 5: Delete the now-unused buildStreamSettings function (lines 165-181)**

Remove the old `buildStreamSettings` function (it's been replaced by `BuildStreamSettings` with `StreamOpts` in service/proxies.go).

- [ ] **Step 6: Verify compilation and run existing tests**

```bash
cd go-backend && go build ./... && go test ./internal/service/ -v -run "TestParseVMess|TestExtractLinks|TestAssignTags|TestSkipUnsupported|TestB64Decode"
```

Expected: PASS (all existing parsing tests still pass)

- [ ] **Step 7: Commit**

```bash
git add go-backend/internal/service/xray_sub.go go-backend/internal/service/proxies.go
git commit -m "refactor: delegate xray_sub parsers to shared Build*Outbound functions"
```

---

### Task 5: Add tests for Build*Outbound functions

**Files:**
- Modify: `go-backend/internal/service/service_test.go`

- [ ] **Step 1: Add test functions at end of file**

```go
func TestBuildVMessOutbound(t *testing.T) {
	ob := BuildVMessOutbound("1.2.3.4", 443, "test-uuid", StreamOpts{
		Network: "ws", Security: "tls", SNI: "example.com", Path: "/ws",
	})
	if ob["protocol"] != "vmess" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	settings := ob["settings"].(map[string]any)
	vnext := settings["vnext"].([]any)
	first := vnext[0].(map[string]any)
	if first["address"] != "1.2.3.4" {
		t.Errorf("address = %v", first["address"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["security"] != "tls" {
		t.Errorf("security = %v", stream["security"])
	}
}

func TestBuildVLessOutbound(t *testing.T) {
	ob := BuildVLessOutbound("2.2.2.2", 443, "uuid", "xtls-rprx-vision", StreamOpts{
		Network: "tcp", Security: "reality", SNI: "yahoo.com",
		Fingerprint: "chrome", PublicKey: "pubkey", ShortId: "abc",
	})
	if ob["protocol"] != "vless" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["security"] != "reality" {
		t.Errorf("security = %v", stream["security"])
	}
	rs := stream["realitySettings"].(map[string]any)
	if rs["publicKey"] != "pubkey" {
		t.Errorf("publicKey = %v", rs["publicKey"])
	}
}

func TestBuildTrojanOutbound(t *testing.T) {
	ob := BuildTrojanOutbound("3.3.3.3", 443, "password", StreamOpts{
		Network: "grpc", Security: "tls", SNI: "example.com", Path: "myservice",
	})
	if ob["protocol"] != "trojan" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	stream := ob["streamSettings"].(map[string]any)
	if stream["network"] != "grpc" {
		t.Errorf("network = %v", stream["network"])
	}
}

func TestBuildSSOutbound(t *testing.T) {
	ob := BuildSSOutbound("4.4.4.4", 8388, "aes-256-gcm", "password")
	if ob["protocol"] != "shadowsocks" {
		t.Errorf("protocol = %v", ob["protocol"])
	}
	settings := ob["settings"].(map[string]any)
	servers := settings["servers"].([]any)
	s := servers[0].(map[string]any)
	if s["method"] != "aes-256-gcm" {
		t.Errorf("method = %v", s["method"])
	}
}

func TestBuildStreamSettingsDefaults(t *testing.T) {
	stream := BuildStreamSettings(StreamOpts{})
	if stream["network"] != "tcp" {
		t.Errorf("default network should be tcp, got %v", stream["network"])
	}
	if stream["security"] != "none" {
		t.Errorf("default security should be none, got %v", stream["security"])
	}
}
```

- [ ] **Step 2: Run tests**

```bash
cd go-backend && go test ./internal/service/ -v -run "TestBuild"
```

Expected: PASS (5 new tests)

- [ ] **Step 3: Run all existing tests to confirm nothing broken**

```bash
cd go-backend && go test ./... -count=1
```

Expected: PASS (all packages)

- [ ] **Step 4: Commit**

```bash
git add go-backend/internal/service/service_test.go
git commit -m "test: add unit tests for Build*Outbound and BuildStreamSettings"
```

---

### Task 6: Update buildProxy in handler/proxies.go for manual path

**Files:**
- Modify: `go-backend/internal/handler/proxies.go:108-153`

- [ ] **Step 1: Replace buildProxy function**

Replace `buildProxy` (lines 108-153):

```go
func buildProxy(body *model.ProxyIn) (*model.Proxy, error) {
	px := &model.Proxy{
		Protocol: body.Protocol,
		Host:     strings.TrimSpace(body.Host),
		Port:     body.Port,
		Auth:     toModelAuth(body.Auth),
		Link:     strings.TrimSpace(body.Link),
	}

	// link paste path — parse share link, extract full outbound
	if px.Link != "" {
		links, _ := service.ExtractLinks(px.Link)
		if len(links) == 0 {
			return nil, fmt.Errorf("无法解析分享链接")
		}
		nodes, skipped := service.ParseLinks(links)
		if len(nodes) == 0 {
			if len(skipped) > 0 {
				return nil, fmt.Errorf("链接解析失败: %s", skipped[0].Detail)
			}
			return nil, fmt.Errorf("未识别到有效协议")
		}
		n := nodes[0]
		px.Protocol = n.Type
		px.Host = n.Host
		px.Port = n.Port
		px.Name = n.Name
		px.RawOutbound = n.Outbound
		return px, nil
	}

	// manual fill path
	switch px.Protocol {
	case "socks", "http":
		if px.Host == "" {
			return nil, fmt.Errorf("代理地址(host)不能为空")
		}
		if px.Port == 0 {
			return nil, fmt.Errorf("端口不能为0")
		}
		return px, nil
	case "vmess", "vless", "trojan", "shadowsocks":
		if body.Host == "" {
			return nil, fmt.Errorf("地址不能为空")
		}
		if body.Port == 0 {
			return nil, fmt.Errorf("端口不能为空")
		}
		if body.UUID == "" && body.Protocol != "shadowsocks" {
			return nil, fmt.Errorf("%s 需要填写 UUID/密码", body.Protocol)
		}
		if body.Protocol == "shadowsocks" {
			if body.Method == "" {
				return nil, fmt.Errorf("shadowsocks 需要选择加密方式")
			}
			if body.UUID == "" {
				return nil, fmt.Errorf("shadowsocks 需要填写密码")
			}
			px.RawOutbound = service.BuildSSOutbound(px.Host, px.Port, body.Method, body.UUID)
			return px, nil
		}
		stream := service.StreamOpts{
			Network:       body.Network,
			Security:      body.TLS,
			SNI:           body.SNI,
			Path:          body.Path,
			Host:          body.WsHost,
			Fingerprint:   body.Fingerprint,
			PublicKey:     body.PublicKey,
			ShortId:       body.ShortId,
			SpiderX:       body.SpiderX,
			AllowInsecure: body.AllowInsecure,
		}
		switch body.Protocol {
		case "vmess":
			px.RawOutbound = service.BuildVMessOutbound(px.Host, px.Port, body.UUID, stream)
		case "vless":
			px.RawOutbound = service.BuildVLessOutbound(px.Host, px.Port, body.UUID, body.Flow, stream)
		case "trojan":
			px.RawOutbound = service.BuildTrojanOutbound(px.Host, px.Port, body.UUID, stream)
		}
		return px, nil
	default:
		return nil, fmt.Errorf("不支持的协议: %s", px.Protocol)
	}
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd go-backend && go build ./...
```

Expected: PASS

- [ ] **Step 3: Run handler tests**

```bash
cd go-backend && go test ./internal/handler/ -v
```

Expected: PASS (4 existing tests)

- [ ] **Step 4: Commit**

```bash
git add go-backend/internal/handler/proxies.go
git commit -m "feat: add manual fill path to buildProxy for vmess/vless/trojan/ss"
```

---

### Task 7: Update frontend Proxies.vue with Tab switching and manual form

**Files:**
- Modify: `frontend/src/views/outbound/Proxies.vue`

- [ ] **Step 1: Replace the entire file**

Replace the current Proxies.vue content:

```vue
<script setup>
import { ref, onMounted, reactive, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { proxyApi } from '../../api/index.js'
import { apiError } from '../../api/http.js'
import { usePanel } from '../../stores/panel.js'

const emit = defineEmits(['changed'])
const panel = usePanel()
const list = ref([])
const dialog = ref(false)
const editing = ref(null)
const mode = ref('manual')
const form = reactive({
  name: '', protocol: 'socks', host: '', port: null, user: '', pass: '',
  link: '', uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
  sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
  publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
})

const protocols = [
  { label: 'socks', value: 'socks' },
  { label: 'http', value: 'http' },
  { label: 'vmess', value: 'vmess' },
  { label: 'vless', value: 'vless' },
  { label: 'trojan', value: 'trojan' },
  { label: 'shadowsocks', value: 'shadowsocks' },
]

const ssMethods = ['aes-256-gcm', 'chacha20-ietf-poly1305', 'aes-128-gcm', 'none']
const networkOpts = ['tcp', 'ws', 'grpc', 'h2']
const tlsOpts = ['none', 'tls', 'reality']
const fpOpts = ['chrome', 'firefox', 'safari', 'edge', 'ios', 'android', 'random']

const isSimple = computed(() => form.protocol === 'socks' || form.protocol === 'http')
const showModeSwitch = computed(() => !isSimple.value && !editing.value?.link)
const isReality = computed(() => form.tls === 'reality')
const isTLS = computed(() => form.tls === 'tls')
const isWS = computed(() => form.network === 'ws')
const isGRPC = computed(() => form.network === 'grpc')
const isVless = computed(() => form.protocol === 'vless')
const isSS = computed(() => form.protocol === 'shadowsocks')

async function load() { list.value = (await proxyApi.list()).data }
onMounted(load)

function resetForm() {
  Object.assign(form, {
    name: '', protocol: 'socks', host: '', port: null, user: '', pass: '',
    link: '', uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
    sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
    publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
  })
  mode.value = 'manual'
}

function openCreate() {
  editing.value = null
  resetForm()
  dialog.value = true
}

function openEdit(row) {
  editing.value = row.tag
  Object.assign(form, {
    name: row.name, protocol: row.protocol, host: row.host || '', port: row.port || null,
    user: row.auth?.user || '', pass: row.auth?.pass || '', link: row.link || '',
    uuid: '', method: 'aes-256-gcm', network: 'tcp', tls: 'none',
    sni: '', path: '', wsHost: '', flow: '', fingerprint: 'chrome',
    publicKey: '', shortId: '', spiderX: '', allowInsecure: false,
  })
  mode.value = row.link ? 'link' : 'manual'
  dialog.value = true
}

function payload() {
  const p = { name: form.name.trim(), protocol: form.protocol }
  if (mode.value === 'link') {
    p.link = form.link.trim()
  } else {
    p.host = form.host.trim()
    p.port = form.port
    if (form.user.trim() || form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
    if (!isSimple.value) {
      p.uuid = form.uuid.trim()
      p.network = form.network
      p.tls = form.tls
      p.sni = form.sni.trim()
      p.path = form.path.trim()
      p.ws_host = form.wsHost.trim()
      p.fingerprint = form.fingerprint
      p.allow_insecure = form.allowInsecure
      if (isVless.value) p.flow = form.flow.trim()
      if (isReality.value) {
        p.public_key = form.publicKey.trim()
        p.short_id = form.shortId.trim()
        p.spider_x = form.spiderX.trim()
      }
      if (isSS.value) p.method = form.method
    }
  }
  return p
}

async function save() {
  try {
    if (editing.value) await proxyApi.update(editing.value, payload())
    else await proxyApi.create(payload())
    dialog.value = false; await load(); await panel.refreshOutbounds(); emit('changed'); ElMessage.success('已保存')
  } catch (e) { ElMessage.error(apiError(e)) }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`删除代理「${row.name}」?`, '确认', { type: 'warning' })
    await proxyApi.remove(row.tag); await load(); await panel.refreshAll(); emit('changed')
  } catch (e) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(apiError(e))
  }
}
</script>

<template>
  <el-card>
    <template #header>
      <div class="hd"><span>自定义出口代理(落地代理)</span>
        <el-button type="primary" @click="openCreate">+ 新建代理</el-button></div>
    </template>
    <el-table :data="list">
      <el-table-column prop="tag" label="tag" width="80" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="protocol" label="协议" width="110" />
      <el-table-column label="地址" width="200"><template #default="{ row }">
        <span v-if="row.host">{{ row.host }}:{{ row.port }}</span>
        <span v-else-if="row.link" style="color:var(--el-text-color-secondary);font-size:12px">{{ row.link.slice(0,40) }}…</span>
      </template></el-table-column>
      <el-table-column label="操作" width="140"><template #default="{ row }">
        <el-button size="small" @click="openEdit(row)">编辑</el-button>
        <el-button size="small" type="danger" @click="remove(row)">删</el-button></template></el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialog" :title="editing ? '编辑代理' : '新建代理'" width="560px">
    <el-form label-width="100px">
      <el-form-item label="名称"><el-input v-model="form.name" placeholder="可选" /></el-form-item>
      <el-form-item label="协议"><el-select v-model="form.protocol">
        <el-option v-for="p in protocols" :key="p.value" :label="p.label" :value="p.value" />
      </el-select></el-form-item>

      <!-- Tab switch for complex protocols -->
      <el-tabs v-if="!isSimple" v-model="mode" class="mode-tabs">
        <el-tab-pane label="手动填写" name="manual" />
        <el-tab-pane label="粘贴链接" name="link" />
      </el-tabs>

      <!-- Link paste panel -->
      <template v-if="mode === 'link' && !isSimple">
        <el-form-item label="分享链接">
          <el-input v-model="form.link" type="textarea" :rows="3" placeholder="粘贴 vmess:// 或 vless:// 或 trojan:// 或 ss:// 链接" />
        </el-form-item>
      </template>

      <!-- Manual fill panel -->
      <template v-if="mode === 'manual' || isSimple">
        <el-form-item label="地址"><el-input v-model="form.host" placeholder="host" /></el-form-item>
        <el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item>

        <!-- socks/http auth -->
        <template v-if="isSimple">
          <el-form-item label="账号"><el-input v-model="form.user" placeholder="可选" /></el-form-item>
          <el-form-item label="密码"><el-input v-model="form.pass" placeholder="可选" /></el-form-item>
        </template>

        <!-- vmess/vless/trojan/ss manual fields -->
        <template v-if="!isSimple">
          <el-form-item v-if="isSS" label="加密方式">
            <el-select v-model="form.method">
              <el-option v-for="m in ssMethods" :key="m" :label="m" :value="m" />
            </el-select>
          </el-form-item>
          <el-form-item :label="isSS ? '密码' : 'UUID/密码'">
            <el-input v-model="form.uuid" :placeholder="isSS ? 'shadowsocks密码' : 'UUID或密码'" />
          </el-form-item>
          <el-form-item label="传输协议">
            <el-select v-model="form.network">
              <el-option v-for="n in networkOpts" :key="n" :label="n" :value="n" />
            </el-select>
          </el-form-item>
          <el-form-item label="TLS">
            <el-select v-model="form.tls">
              <el-option v-for="t in tlsOpts" :key="t" :label="t" :value="t" />
            </el-select>
          </el-form-item>

          <!-- Advanced: el-collapse -->
          <el-collapse v-model="[]" style="margin-top:8px">
            <el-collapse-item title="高级配置" name="adv">
              <el-form-item v-if="isVless" label="Flow">
                <el-input v-model="form.flow" placeholder="xtls-rprx-vision" />
              </el-form-item>
              <el-form-item v-if="isTLS" label="SNI">
                <el-input v-model="form.sni" placeholder="默认同地址" />
              </el-form-item>
              <el-form-item v-if="isReality" label="SNI">
                <el-input v-model="form.sni" placeholder="reality 回落域名" />
              </el-form-item>
              <el-form-item :label="isGRPC ? 'ServiceName' : 'Path'">
                <el-input v-model="form.path" :placeholder="isGRPC ? 'grpc服务名' : '/ws-path'" />
              </el-form-item>
              <el-form-item v-if="isWS" label="Host">
                <el-input v-model="form.wsHost" placeholder="ws host header" />
              </el-form-item>
              <el-form-item v-if="isTLS || isReality" label="Fingerprint">
                <el-select v-model="form.fingerprint">
                  <el-option v-for="f in fpOpts" :key="f" :label="f" :value="f" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="isReality" label="PublicKey">
                <el-input v-model="form.publicKey" placeholder="reality 公钥" />
              </el-form-item>
              <el-form-item v-if="isReality" label="ShortId">
                <el-input v-model="form.shortId" placeholder="shortId" />
              </el-form-item>
              <el-form-item v-if="isReality" label="SpiderX">
                <el-input v-model="form.spiderX" placeholder="spiderX" />
              </el-form-item>
              <el-form-item v-if="isTLS" label="AllowInsecure">
                <el-switch v-model="form.allowInsecure" />
              </el-form-item>
            </el-collapse-item>
          </el-collapse>
        </template>
      </template>
    </el-form>
    <template #footer><el-button @click="dialog = false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
.hd { display:flex; justify-content:space-between; align-items:center; }
.mode-tabs { margin-bottom: 8px; }
</style>
```

- [ ] **Step 2: Verify build (frontend)**

```bash
cd frontend && npm run build
```

Expected: build succeeds without errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/views/outbound/Proxies.vue
git commit -m "feat: add Tab switching, manual form with advanced config to proxy dialog"
```

---

### Task 8: Run full test suite and verify

**Files:** (none, verification only)

- [ ] **Step 1: Run all Go tests**

```bash
cd go-backend && go test ./... -count=1 -v 2>&1 | tail -30
```

Expected: PASS (all packages, ~50 tests)

- [ ] **Step 2: Verify Docker build**

```bash
docker build -t xray-panel:test . 2>&1 | tail -5
```

Expected: build succeeds

- [ ] **Step 3: Commit any final adjustments**

```bash
git add -A && git commit -m "chore: final verification after proxy dual-mode implementation" || echo "no changes to commit"
```

---

## Verification Checklist

After implementation, verify via Chrome DevTools:

1. **手动填写 vmess** — 填地址、端口、UUID、ws、tls → 保存 → 列表可见
2. **粘贴链接 vmess** — 粘贴 vmess:// 链接 → 保存 → 列表可见，地址栏显示解析结果
3. **手动填写 vless + reality** — 填 UUID、reality 参数 → 保存成功
4. **手动填写 trojan** — 填地址、端口、密码 → 保存成功
5. **手动填写 shadowsocks** — 选加密、填密码 → 保存成功
6. **Tab 切换** — 手动填写 tab ↔ 粘贴链接 tab，数据保留不丢失
7. **编辑回填** — 编辑手动创建的 proxy → 表单正确回填，Tab 定位到 manual
8. **link 优先** — 同时填了 link 和手动字段 → 后端走 link 解析路径
