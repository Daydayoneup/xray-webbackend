# xray-panel — Xray-core Web 管理面板

## 项目概述

轻量级 Xray-core 服务器管理面板，为 Xray-core 提供浏览器端的图形化管理界面。
单二进制部署，Docker 镜像约 25MB，内存占用 10-20MB。

## 技术栈

| 层 | 技术 | 说明 |
|---|---|---|
| **后端** | Golang (go 1.25) | chi v5 路由 + net/http，标准 Go 项目布局 |
| **前端** | Vue 3 + Element Plus | SPA，构建产物嵌入 Go 二进制 |
| **数据库** | JSON 文件 | 原子写入 (tmp + rename)，0600 权限 |
| **容器化** | Docker 多阶段构建 | xray-core + golang build + node build → scratch |

### Go 依赖

- `github.com/go-chi/chi/v5` — HTTP 路由
- `github.com/caarlos0/env/v11` — 环境变量解析
- `github.com/go-playground/validator/v10` — 请求体校验
- `golang.org/x/crypto` — PBKDF2-SHA256 密码哈希
- `crypto/subtle` — 恒定时间哈希比较（防时序攻击）

## 项目结构

```
├── go-backend/                    # Golang 后端
│   ├── cmd/server/main.go         # 入口：初始化 → 启动 xray → HTTP server → 优雅关闭
│   └── internal/
│       ├── config/config.go       # 环境变量配置 (Settings struct + env tags)
│       ├── model/state.go         # 核心数据模型 (PanelState, Node, Proxy, Rule, Balancer, etc.)
│       ├── model/api.go           # API 请求体 + validator 校验
│       ├── store/store.go         # Store 接口 + JSON 持久化 + 旧格式向后兼容迁移
│       ├── auth/password.go       # PBKDF2-SHA256 密码哈希与验证
│       ├── auth/session.go        # 内存 SessionStore (Bearer token, 7天过期)
│       ├── handler/server.go      # Chi 路由注册 + SPA 静态文件服务
│       ├── handler/helpers.go     # HTTP 工具函数 (writeJSON, writeError, decodeJSON)
│       ├── handler/auth.go        # 登录/登出/当前用户/修改密码
│       ├── handler/inbounds.go    # 入站 CRUD (socks/http)
│       ├── handler/proxies.go     # 落地代理 CRUD + 分享链接自动解析
│       ├── handler/balancers.go   # 负载均衡器 CRUD
│       ├── handler/routing.go     # 分流规则、路由模板、出站标签列表
│       ├── handler/subscription.go # 多订阅管理 (增删/单条拉取/全部拉取) + SSRF 防护
│       ├── handler/xray.go        # Xray 状态/应用/重启/配置查看/拓扑
│       ├── middleware/auth.go     # Bearer Token 鉴权中间件
│       ├── service/xray_proc.go   # XrayProc 接口 + os/exec 实现 + Fake 实现(测试用)
│       ├── service/xray_sub.go    # 订阅解析: vmess/vless/trojan/ss 四种协议 + TCP 延迟测速
│       ├── service/config_builder.go # PanelState → Xray JSON 配置生成
│       ├── service/inbounds.go    # Inbound → Xray 配置转换
│       ├── service/proxies.go     # Proxy → Xray 配置转换 (socks/http)
│       ├── service/routing.go     # Rule → Xray 配置转换 + 路由模板
│       └── app/app.go            # App 聚合：密码初始化、标签管理、悬空引用清理、dirty 检测
├── frontend/                      # Vue 3 前端
│   └── src/
│       ├── views/
│       │   ├── Login.vue          # 登录页
│       │   ├── Dashboard.vue      # 仪表盘 (节点列表/配置状态/拓扑)
│       │   ├── Inbound.vue        # 入站管理
│       │   ├── outbound/
│       │   │   ├── Proxies.vue    # 落地代理 (支持 6 种协议)
│       │   │   └── Subscription.vue # 订阅管理 (多订阅)
│       │   ├── Routing.vue        # 分流规则编辑
│       │   └── Settings.vue       # 修改密码
│       ├── api/index.js           # API 客户端 (CRUD 工厂 + 通用错误处理)
│       ├── api/http.js            # Axios 封装
│       └── stores/
│           ├── auth.js            # 登录状态管理
│           └── panel.js           # 面板状态管理
├── Dockerfile                     # 多阶段构建: xray + go + node → scratch
└── docker-compose.yml             # host 网络模式，挂载 ./data
```

## 核心架构

### 分层设计

```
cmd/server/main.go         ← 入口：组装依赖、启动服务
        │
   ┌────▼────┐
   │ handler │  ← HTTP 层：路由注册、请求解析、参数校验、响应格式化
   └────┬────┘
   ┌────▼────┐
   │   app   │  ← 业务聚合层：状态管理、标签查询、dirty 检测、悬空引用清理
   └────┬────┘
   ┌────▼────┐
   │ service │  ← 领域服务层：Xray 进程管理、订阅解析、配置构建
   └────┬────┘
   ┌────▼────┐
   │  store  │  ← 持久化层：JSON 文件读写、原子写入、向后兼容迁移
   └─────────┘
```

### 关键接口抽象

```go
// 持久化（store/store.go）
type Store interface {
    Load() (*model.PanelState, error)
    Save(state *model.PanelState) error
    Lock()   // 写互斥锁
    Unlock()
}

// Xray 进程管理（service/xray_proc.go）
type XrayProc interface {
    Start(configPath string)
    Stop()
    Status() string
    Restart() error
    Apply(configPath string) error
    AppliedConfig() map[string]any
}
```

两个接口都有对应的 Fake 实现用于测试。

### 数据模型 (model/state.go)

- **PanelState** — 根状态对象，包含所有子资源
  - `Password` — PBKDF2 哈希后的密码记录
  - `Subscriptions` — 订阅 URL 列表（支持多个）
  - `Nodes` — 从订阅解析出的出站节点（vmess/vless/trojan/ss）
  - `Inbounds` — 本地入站配置（socks/http）
  - `Proxies` — 自定义落地代理（支持 6 种协议）
  - `Balancers` — 负载均衡组（leastPing 等策略）
  - `Rules` — 分流规则（domain-suffix/geosite/geoip/port 等）
  - `DefaultOutbound` — 默认出站标签

## 支持的功能

### 订阅管理
- 多订阅地址支持（增删/单独拉取/一键拉取全部）
- 自动解析 vmess、vless、trojan、shadowsocks 四种协议
- 支持 base64 编码的分享链接
- 节点去重（按 host:port）
- SSRF 防护（DNS 预解析 + 内网地址拒绝 + 不跟随重定向）
- TCP 延迟测速（32 并发 goroutine，3s 超时）

### 入站管理
- SOCKS/HTTP 代理端口配置
- 可选的账号密码鉴权
- UDP 支持开关

### 出站管理
- 查看/管理所有节点
- 6 种协议落地代理：socks/http（手动填写）、vmess/vless/trojan/shadowsocks（粘贴链接自动解析）
- 负载均衡组（selector + 策略）

### 分流规则
- 支持类型：domain-suffix、full、keyword、geosite、geoip、port
- 规则启用/禁用开关
- 预设路由模板
- 悬空引用自动清理（删除出站时联动清理关联规则）

### 配置管理
- 修改后一键校验并应用 Xray 配置
- 配置校验失败时拦截并提示错误
- Dirty 状态检测（对比草稿与已生效配置）
- 支持查看当前生效的原始 JSON 配置

### 安全
- PBKDF2-SHA256 密码哈希 + crypto/subtle.ConstantTimeCompare（防时序攻击）
- Bearer Token 会话管理（7 天过期，内存存储）
- 凭据文件 0600 权限
- 订阅拉取 SSRF 防护
- 支持环境变量/随机生成两种密码初始化方式

## API 路由

全部路由注册在 `handler/server.go` 的 `Routes()` 方法中。

鉴权路由（Bearer Token 中间件保护）：
- `/api/auth/*` — 登出、当前用户、修改密码
- `/api/inbounds` — 入站 CRUD
- `/api/proxies` — 落地代理 CRUD
- `/api/balancers` — 负载均衡 CRUD
- `/api/routing` — 分流规则、模板、出站标签
- `/api/subscriptions` — 订阅 CRUD + 单条/全部拉取
- `/api/nodes` — 节点列表 + 延迟测试
- `/api/xray/*` — 状态、应用、重启、配置、拓扑

公开路由：
- `POST /api/auth/login` — 登录

SPA fallback：非 API 路径返回 `index.html`。

## 测试

```bash
cd go-backend && go test ./...
```

42 个测试覆盖 5 个包（config、model、auth、store、handler、service），所有接口抽象都有 Fake 实现支持测试。

## 部署

```bash
docker compose up -d --build
# 访问 http://<IP>:2017
```

关键环境变量：`PANEL_PORT`、`PANEL_PASSWORD`、`XRAY_BIN`、`PANEL_DATA_DIR`、`SUBSCRIPTION_ALLOW_INTERNAL`。

## 代码规范

- 错误信息使用中文（与前端 UI 语言一致）
- 日志使用 `log/slog`
- JSON 序列化：Auth.Password ↔ JSON key "pass"（与 Python 版兼容）
- 原子文件写入：先写 `.tmp`，再 rename
- 状态迁移在 Load 时完成，兼容旧格式
