# 代理添加表单：手动填写 + 粘贴链接 双模式设计

> **目标**：所有 6 种协议（socks/http/vmess/vless/trojan/shadowsocks）统一支持手动填写和粘贴链接两种添加方式，用户通过 Tab 切换。

## 交互设计

### 协议覆盖

| 协议 | 手动填写 | 粘贴链接 | 说明 |
|---|---|---|---|
| socks | ✅ | ❌ | 无标准分享格式，仅手动填写 |
| http | ✅ | ❌ | 无标准分享格式，仅手动填写 |
| vmess | ✅ | ✅ | vmess:// base64 JSON |
| vless | ✅ | ✅ | vless:// URL |
| trojan | ✅ | ✅ | trojan:// URL |
| shadowsocks | ✅ | ✅ | ss:// URL |

### UI 布局

```
┌─ 名称 ─────────────────────────────────┐
│  [input, 可选，手动模式下自动用 host 填充]  │
├─ 协议 ─────────────────────────────────┤
│  [select: socks|http|vmess|vless|      │
│           trojan|shadowsocks]          │
└────────────────────────────────────────┘

[socks/http]: 仅显示手动填写区域（无 Tab）

[vmess/vless/trojan/ss]: 显示 Tab 切换
  ┌─── 手动填写 ───┐  ┌─── 粘贴链接 ───┐
  │                │  │                │

手动填写面板 (Tab = manual):
┌───────────────────────────────────────┐
│  基础参数                              │
│  地址: [____]      端口: [____]        │
│                                       │
│  [vmess] UUID: [____]                │
│  [vless] UUID: [____]                │
│  [trojan] 密码: [____]               │
│  [ss] 加密方式: [select]  密码: [___] │
│                                       │
│  传输协议: [tcp|ws|grpc|h2 ▾]         │
│  TLS: [none|tls|reality ▾]           │
│                                       │
│  ▼ 高级配置 (el-collapse, 默认折叠)     │
│    SNI: [____]                       │
│    Path: [____]                      │
│    Host: [____]                      │
│    Fingerprint: [____]               │
│    Flow: [____]              (vless) │
│    PublicKey: [____]         (reality)│
│    ShortId: [____]           (reality)│
│    SpiderX: [____]           (reality)│
│    AllowInsecure: [checkbox]         │
└───────────────────────────────────────┘

粘贴链接面板 (Tab = link):
┌───────────────────────────────────────┐
│  分享链接:                             │
│  [textarea, 3行]                      │
│                                       │
│  解析结果预览:                          │
│  host:port (protocol)                 │
└───────────────────────────────────────┘
```

### 行为规则

1. **协议切换时保留模式**：从 vmess 切换到 trojan 时，如果当前在手动填写 tab，保持在手动填写
2. **模式记忆**：编辑时，如果原始 proxy 有 `link` 字段，默认定位到粘贴链接 tab；否则定位到手动填写 tab
3. **字段联动**：
   - 传输协议选 grpc 时，path 标签改为"serviceName"
   - TLS 选 reality 时，显示 PublicKey/ShortId/SpiderX；选 tls 时，显示 SNI/AllowInsecure/Fingerprint
   - 协议选 vless 时，显示 Flow 字段；其他协议隐藏
   - 协议选 ss 时，地址和端口从 base64 userinfo 区域拆分输入改为直接输入
4. **提交优先级**：后端 `buildProxy()` 中，link 字段优先 → 有 link 走解析路径；无 link 走手动填写路径

## 后端改动

### model/api.go — ProxyIn 扩展字段

```go
type ProxyIn struct {
    Name     string  `json:"name"`
    Protocol string  `json:"protocol" validate:"required,oneof=socks http vmess vless trojan shadowsocks"`
    Host     string  `json:"host"`
    Port     int     `json:"port" validate:"omitempty,min=1,max=65535"`
    Auth     *AuthIn `json:"auth" validate:"omitempty"`
    Link     string  `json:"link"`

    // 手动填写 — vmess/vless/trojan/ss 专用
    UUID          string `json:"uuid"`
    Method        string `json:"method"`        // ss: none, aes-256-gcm, chacha20-ietf-poly1305
    Network       string `json:"network"`       // tcp, ws, grpc, h2
    TLS           string `json:"tls"`           // none, tls, reality
    SNI           string `json:"sni"`
    Path          string `json:"path"`
    WsHost        string `json:"ws_host"`
    Flow          string `json:"flow"`          // vless xtls-rprx-vision
    Fingerprint   string `json:"fingerprint"`   // chrome, firefox, safari, etc.
    PublicKey     string `json:"public_key"`    // reality
    ShortId       string `json:"short_id"`      // reality
    SpiderX       string `json:"spider_x"`      // reality
    AllowInsecure bool   `json:"allow_insecure"`
}
```

验证规则：
- 手动填写模式下，Host 和 Port 变为 required（所有协议）
- socks/http 保持现有 required host/port 逻辑
- UUID 在手动填写 + vmess/vless/trojan 时为 required
- Method 在手动填写 + ss 时为 required

### handler/proxies.go — buildProxy 逻辑调整

```
func buildProxy(body *ProxyIn) (*Proxy, error):
    if body.Link != "":
        → 走现有解析路径（ExtractLinks + ParseLinks）
        → 返回 parsed proxy

    // 手动填写路径
    switch body.Protocol:
        case "socks", "http":
            → 现有手动逻辑不变
        case "vmess", "vless", "trojan", "shadowsocks":
            → validate required manual fields (host, port, uuid/method)
            → 调用 BuildOutboundFromManual(body) 构建 RawOutbound
```

### service/proxies.go — 新增 BuildOutboundFromManual

抽取 link 解析器和手动构建的公共逻辑：

- 将 `parseVMess` 中构建 outbound map 的部分提取为 `BuildVMessOutbound(host, port, uuid, network, tls, ...)` 
- 同理抽取 `BuildVLessOutbound` / `BuildTrojanOutbound` / `BuildSSOutbound`
- 现有 `parseVMess` 等函数内部改为：解析 URL 参数 → 调用 Build*Outbound

这样 link 解析和手动填写走同一套 outbound 构建逻辑，避免代码重复。

## 前端改动

### stores/panel.js — 不需要改动

### views/outbound/Proxies.vue 改动

**新增状态：**
```javascript
const mode = ref('manual')  // 'manual' | 'link'
```

**新增方法：**
```javascript
// 协议切换时处理 mode 可见性
function showModeSwitch() {
  return !isSimple()  // socks/http 为简单协议，不显示 tab
}
// 编辑时回填 mode
function openEdit(row) {
  // ...现有回填逻辑
  mode.value = row.link ? 'link' : 'manual'
}
```

**模板变化：**
- 协议选择器下方增加 `<el-tabs v-if="showModeSwitch()" v-model="mode">`
- manual tab: 基础参数 + `<el-collapse>` 包裹高级参数
- link tab: 复用现有 textarea 逻辑
- 字段根据协议动态显示/隐藏（TLS → SNI 显示逻辑，grpc → serviceName 标签切换等）

**payload() 调整：**
```javascript
function payload() {
  const p = { name, protocol }
  if (mode.value === 'link') {
    p.link = form.link.trim()
  } else {
    p.host = form.host.trim()
    p.port = form.port
    // vmess/vless/trojan/ss 手动字段
    if (!isSimple()) {
      p.uuid = form.uuid
      p.network = form.network
      p.tls = form.tls
      // 高级字段
      if (showAdvanced.value) {
        p.sni = form.sni
        p.path = form.path
        p.ws_host = form.wsHost
        p.flow = form.flow
        p.fingerprint = form.fingerprint
        p.public_key = form.publicKey
        p.short_id = form.shortId
        p.spider_x = form.spiderX
        p.allow_insecure = form.allowInsecure
      }
    }
    // socks/http 和 vmess/vless/trojan 都需要 auth
    if (form.user && form.pass) p.auth = { user: form.user.trim(), pass: form.pass }
  }
  return p
}
```

## 测试计划

### 后端测试

1. `TestBuildProxyManualVMess` — 手动填写 vmess 基础参数，验证生成的 RawOutbound
2. `TestBuildProxyManualVLess` — 手动填写 vless + flow，验证 xtls-rprx-vision
3. `TestBuildProxyManualTrojan` — 手动填写 trojan
4. `TestBuildProxyManualSS` — 手动填写 ss + method
5. `TestBuildProxyManualWithAdvanced` — 高级配置 (ws + tls + sni + path)
6. `TestBuildProxyManualReality` — reality 配置 (pbk + sid + spx + fp)
7. `TestBuildProxyLinkPriority` — link 非空时优先走解析路径，忽略手动字段
8. `TestProxyInValidationManual` — 手动模式下的 required 字段校验

### 前端验证

- Chrome DevTools 验证 6 种协议手动填写 → 保存 → 编辑回填
- Tab 切换时表单数据保留不丢失
- 高级配置折叠/展开正常
- 字段联动正确（tls → sni, grpc → serviceName, vless → flow）

## 文件变更清单

| 文件 | 操作 | 说明 |
|---|---|---|
| `go-backend/internal/model/api.go` | 修改 | ProxyIn 新增手动填写字段 |
| `go-backend/internal/model/api_test.go` | 修改 | 新增手动模式验证测试 |
| `go-backend/internal/handler/proxies.go` | 修改 | buildProxy 增加手动填写分支 |
| `go-backend/internal/service/proxies.go` | 修改 | 新增 Build*Outbound 公共函数，重构解析器 |
| `go-backend/internal/service/xray_sub.go` | 修改 | parseVMess 等改为调用 Build*Outbound |
| `frontend/src/views/outbound/Proxies.vue` | 修改 | Tab 切换、手动表单、字段联动、payload 调整 |
