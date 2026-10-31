# xray-panel — Xray-core Web 管理面板

## 项目概述

轻量级 Xray-core 服务器管理面板，为 Xray-core 提供浏览器端的图形化管理界面。
单二进制部署（前端经 go:embed 嵌入），支持一键安装脚本（systemd）与 Docker 两种部署，内存占用 10-20MB。

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
│   ├── cmd/server/main.go         # 入口：子命令 passwd / install-xray / version；默认启动面板（找不到 Xray 时后台自动安装）
│   └── internal/
│       ├── webui/webui.go         # go:embed 前端（dist/ 由构建流程填充，空时回退到 ./frontend_dist）
│       ├── app/password.go        # 初始密码（initial-password.txt）、passwd 重置文件（password-reset.json）、初始密码提醒
│       ├── app/xrayinstall.go     # ResolveXrayBin（XRAY_BIN → 数据目录 bin/xray → PATH）与自动安装状态
│       ├── service/xrayinstall.go # 下载官方 Xray Release zip，校验 .dgst SHA-256，解压 xray + geo 数据
│       ├── config/config.go       # 环境变量配置 (Settings struct + env tags)；可选 /etc/xray-panel/config.env（PANEL_CONFIG）
│       ├── model/state.go         # 核心数据模型 (PanelState, Node, Proxy, Rule, Balancer, etc.)
│       ├── model/api.go           # API 请求体 + validator 校验
│       ├── store/store.go         # Store 接口 + JSON 持久化 + 旧格式向后兼容迁移
│       ├── auth/password.go       # PBKDF2-SHA256 密码哈希与验证
│       ├── auth/session.go        # 内存 SessionStore (Bearer token, 7天过期)
│       ├── handler/server.go      # Chi 路由注册 + SPA 静态文件服务
│       ├── handler/helpers.go     # HTTP 工具函数 (writeJSON, writeError, decodeJSON)
│       ├── handler/auth.go        # 登录/登出/当前用户/修改密码
│       ├── handler/inbounds.go    # 入站 CRUD（socks/http/vless/ss2022）、关联用户、独立 DNS
│       ├── handler/users.go       # 用户 CRUD + 分享链接（一个用户关联多个入站）
│       ├── handler/rulesets.go    # 域名/IP 分组 CRUD、内置 geosite/geoip 分类浏览
│       ├── handler/proxies.go     # 自定义代理 CRUD + 分享链接自动解析
│       ├── handler/balancers.go   # 负载均衡器 CRUD
│       ├── handler/routing.go     # 分流规则、路由模板、出站标签列表
│       ├── handler/subscription.go # 订阅 CRUD/拉取 + 节点列表/启停/测速
│       ├── handler/xray.go        # Xray 状态/应用/重启/配置查看/拓扑
│       ├── middleware/auth.go     # Bearer Token 鉴权中间件
│       ├── model/spec.go          # OutboundSpec：节点/自定义代理的结构化出站参数（持久化的“用户意图”）
│       ├── service/xray_proc.go   # XrayProc：进程守护(崩溃检测/自动重启/日志缓冲/版本) + Fake 实现
│       ├── service/probe.go       # 真实延迟测速：临时 Xray 实例逐节点请求 generate_204
│       ├── service/xray_sub.go    # 分享链接解析 → OutboundSpec (vmess/vless/trojan/ss) + TCP 测速
│       ├── service/subfetch.go    # 订阅拉取：SSRF 防护、状态码/大小检查、subscription-userinfo、Clash 识别
│       ├── service/outbound.go    # OutboundSpec → Xray 出站 JSON（唯一生成入口）、指纹、旧数据还原
│       ├── service/config_builder.go # PanelState → Xray JSON + 警告（不可用出口跳过/回退、链式代理）
│       ├── service/inbounds.go    # Inbound → Xray 配置转换、分享链接
│       ├── service/users.go       # 用户 → 各入站凭据（vless UUID / ss 派生密钥 / socks 密码）
│       ├── service/ruleset.go     # 分组条目规范化（域名/IP/geosite/geoip）与 Xray 写法转换
│       ├── service/rulelist.go    # 远程规则列表下载与解析（GFWList/AdBlock、Clash 规则集、域名列表）
│       ├── app/rulesets.go        # 订阅分组的更新（失败保留原内容）与定时更新
│       ├── service/routing.go     # Rule → Xray 配置转换（多值）、规则值校验、路由模板
│       ├── service/dns.go         # DNS 设置 → dns 段（系统/国内外分流/DoH/自定义）
│       ├── service/metrics.go     # 读取 Xray metrics（/debug/vars）：流量计数 + observatory 探测结果
│       ├── service/access.go      # 解析 Xray 访问日志（来源 IP / 入站 / 用户 / 目标）
│       ├── service/geodata.go     # geoip/geosite.dat 按需解析（protowire），供规则测试使用
│       ├── service/ruletest.go    # 规则测试：在生成配置上模拟 Xray 路由（含 IPIfNonMatch 两轮匹配）
│       ├── service/crypto.go      # UUID / reality X25519 密钥 / ss2022 密钥
│       ├── service/notify.go      # 通知：飞书 / 钉钉 / 企业微信（含签名）/ Telegram / Bark / 通用 Webhook
│       ├── service/diff.go        # 配置逐行 diff
│       ├── app/app.go             # App 聚合：引用统计、悬空引用清理、失效节点回收、dirty 检测
│       ├── app/subscriptions.go   # 订阅刷新：按指纹沿用节点 tag、失败保留节点、消失节点标记失效
│       ├── app/apply.go           # 事务式应用（校验→备份→重启→失败回滚）+ 订阅自动更新调度
│       ├── app/history.go         # 每次应用保存快照（最近 10 次）、对比、回滚
│       ├── app/backup.go          # 导出/导入全部面板数据（不含密码）
│       ├── app/traffic.go         # 流量采集：累计计数 → 按天增量，存 traffic.json
│       ├── app/monitor.go         # 后台任务 + 提醒（崩溃/订阅失败/到期/流量/节点组全挂/geo 更新/用户上下线）
│       ├── app/online.go          # 在线用户：访问日志 + 流量增量判断上下线；筛选/排序/分页；通知 15 秒合并
│       ├── app/geo.go             # geo 数据下载更新（直连失败自动经本机代理）
│       ├── app/groups.go          # 节点组实时状态（当前使用节点）、手动选择组切换
│       ├── app/users.go           # 用户列表视图（在线地址、今日/30 天流量）
│       └── app/migrate.go         # 数据升级：v2 结构化出站 / v3 订阅排除关键字 / v4 入站多用户 / v5 用户上下线提醒 / v6 入站用户合并为全局用户
├── frontend/                      # Vue 3 前端（components/OnlinePanel.vue：概览页在线用户）
│   └── src/
│       ├── views/
│       │   ├── Login.vue          # 登录页
│       │   ├── Dashboard.vue      # 仪表盘（上手指引/状态/节点组当前节点/流量统计/路由）
│       │   ├── Inbound.vue        # 入站：socks/http/vless+reality/ss2022，关联用户，高级设置（独立 DNS），分享
│       │   ├── Users.vue          # 用户管理：关联入站、凭据、在线状态、流量、分享
│       │   ├── Online.vue         # 在线连接：完整在线列表（搜索/筛选/排序/分页）+ 上下线记录
│       │   ├── outbound/
│       │   │   ├── Proxies.vue    # 自定义代理 (支持 6 种协议)
│       │   │   └── Subscription.vue # 订阅管理 (多订阅)
│       │   ├── Routing.vue        # 分流规则编辑（类型「自定义分组」引用 RuleSet）
│       │   ├── RuleSets.vue       # 域名 / IP 分组：自定义分组编辑 + 内置 geosite/geoip 浏览
│       │   └── Settings.vue       # 设置：账户/服务器地址、DNS、通知、规则数据库、备份与历史、配置查看
│       ├── api/index.js           # API 客户端 (CRUD 工厂 + 通用错误处理)
│       ├── components/TrafficPanel.vue # 流量统计面板（嵌在概览页：按天/出口/入站/用户）
│       ├── components/OnlinePanel.vue  # 在线用户：compact（概览摘要 + 前 10）/ 完整模式（在线连接页）
│       ├── api/http.js            # Axios 封装
│       └── stores/
│           ├── auth.js            # 登录状态管理
│           └── panel.js           # 面板状态管理
├── Dockerfile                     # 多阶段多架构构建: xray + node + go（交叉编译，前端嵌入）→ scratch
├── Makefile                       # make build：前端复制到 go-backend/internal/webui/dist 后构建单文件
├── install.sh                     # 一键安装/升级/卸载：下载 Release 发布包（面板 + 官方 Xray + geo，一个 tar.gz）+ systemd
├── .github/workflows/release.yml  # 唯一流程：测试 → v* 标签构建 linux amd64/arm64/armv7 二进制并与官方 Xray-core（XRAY_VERSION，校验 .dgst）打包 tar.gz + 推送多架构镜像（版本号/latest）→ 创建 Release；main 只推 main/sha 镜像
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
    Running() bool
    Status() ProcStatus                 // running | restarting | crashed | stopped
    Start(configPath string)
    Stop()
    Restart(configPath string) bool     // 重启并观察约 1 秒是否存活
    TestConfig(cfgJSON []byte) (bool, string)
    Logs(n int) []string
    Probe(outbounds []map[string]any, timeout time.Duration) ([]*int, error)
    SetAssetDir(dir string)             // geo 数据目录（XRAY_LOCATION_ASSET）
}
```

两个接口都有对应的 Fake 实现用于测试。

### 数据模型 (model/state.go)

- **PanelState** — 根状态对象，包含所有子资源
  - `Password` — PBKDF2 哈希后的密码记录
  - `Subscriptions` — 订阅 URL 列表（支持多个）
  - `Nodes` — 从订阅解析出的出站节点；`Tag` 稳定不变，`SubID` 标记归属，`Missing` 表示已从订阅消失但仍被引用
  - `Inbounds` — 本地入站配置（socks/http/vless/ss2022），`DNS` 为入站独立 DNS
  - `Users` — 全局用户，`Inbounds` 为关联的入站 tag；Name 即 Xray email（流量/在线统计 key）
  - `Proxies` — 自定义代理（6 种协议），`Via` 为链式代理前置出口
  - `Balancers` — 负载均衡组（leastPing 等策略）
  - `Rules` — 分流规则（domain-suffix/geosite/geoip/port/set 等），`set` 的值为 RuleSet tag；`Name` 可选，生成时写入 Xray `ruleTag`，`Describe` 显示为「名称（条件）」
  - `RuleSets` — 自定义域名/IP 分组（`set-N`），可混用 geosite:/geoip: 引用；`Source` 非空为订阅远程列表（如 GFWList），内容由更新时解析生成
  - `DefaultOutbound` — 默认出站标签
  - `DomainStrategy` — 路由域名策略（默认 IPIfNonMatch）
  - `DNS` / `Notify` / `Geo` / `PublicHost` — DNS、通知、规则数据库、分享链接用的服务器地址
  - `SchemaVersion` — 数据版本（当前 6，见 app/migrate.go）

### 数据目录（$PANEL_DATA_DIR）

- `panel.json` 面板数据；`config.json` 当前生效配置（`.bak` 为上一版）
- `history/` 应用快照；`geo/` 面板下载的 geo 数据（优先于镜像自带）；`traffic.json` 按天流量

### 配置生成要点

- `service.Build(state, BuildOpts{MetricsPort})` 是唯一入口，app 层用 `a.Build()`（自动带部署参数）。
- **兜底规则依赖 domainStrategy**：IPIfNonMatch 时兜底规则匹配 `ip: 0.0.0.0/0, ::/0`，否则第一轮必命中、域名永远不会被解析，geoip 规则失效；AsIs/IPOnDemand 用 `network`。默认出口同时放在 outbounds 第一位。
- 开启 metrics 时注入 `stats`/`policy`/`metrics` 段和 `metrics-in`（dokodemo-door, 127.0.0.1:METRICS_PORT）入站及其路由规则（第一条）。
- 在线用户依赖 Xray 访问日志（默认输出到 stdout）：`XrayProc.SetAccessHandler` 把访问日志行交给 `app/online.go`，不进日志缓冲。Xray 不报告断开，`onlineIdle`（3 分钟）内无新连接且无流量即掉线；流量增量会刷新活跃时间，长连接不会被误判。回调来自 Xray 输出管道，里面不能等 Store 锁（应用配置时持锁等 Xray 退出），通知先入队（`queueOnlineNotify`），15 秒内的事件合并成一条发送。
- 用户：入站的有效用户由 `service.InboundUsers` 从全局用户解析（停用的用户不生成）。ss2022 用户主密钥 32 字节，128 位加密方式取前 16 字节（`SSUserKey`，配置与分享链接共用）。v6 迁移时同名用户合并、同类凭据冲突者改名为 `name-入站tag`，凭据原样保留。
- 分组规则：一条 `set` 规则展开成「域名规则 + IP 规则」两条（同一条 Xray 规则里 domain 与 ip 是“且”），带 `ruleTag: 分组「名称」` 供 Describe 显示；RuleSource 都指向同一条用户规则。删除分组会删除引用它的规则（PruneDangling）。
- 入站独立 DNS：Xray 内置 DNS 是全局的，因此按入站实现为「该入站目标端口 53 的流量 → freedom `redirect` 到指定服务器」，可经节点（`sockopt.dialerProxy`，不支持自动节点组）。规则在用户规则之前。
- 节点组：`GroupCandidates` = 固定成员 + 关键字匹配节点（每次生成时计算，新节点自动加入）；`select` 策略不生成 Xray balancer，引用直接映射到所选出口。

### 关键不变量

- **节点 tag 稳定**：刷新订阅按 严格指纹 → 协议+地址+端口+凭据 → 同订阅同名 三级匹配沿用旧 tag，规则/节点组引用不会串到别的节点。
- **不静默删除引用**：订阅中消失但被引用的节点标记 `Missing` 并保留；无人引用时在 `Persist()` 中回收。只有用户主动删除自定义代理/节点组才会清理相关规则。
- **持久化用户意图**：出站以 `OutboundSpec` 保存，Xray JSON 在应用时由 `service.BuildOutbound` 生成；`Outbound`/`RawOutbound` 仅为兼容旧数据。
- **配置生成不失败**：不可用的出口被跳过/回退，并以 `BuildResult.Warnings` 反馈给前端。

## 支持的功能

### 订阅管理
- 多订阅（增删改/单独更新/全部更新），可命名，可设置自动更新间隔
- 自动解析 vmess、vless、trojan、shadowsocks；传输 tcp/ws/grpc/h2/httpupgrade/xhttp，安全 tls/reality
- 展示流量与到期（subscription-userinfo）、上次失败原因、跳过节点的原因
- 每个订阅可设排除关键字（默认过滤「剩余流量/到期/官网」等信息节点，编辑时实时预览）与节点名称前缀
- 拉取失败保留原节点；节点 tag 稳定；按完整指纹去重
- 节点启用/禁用（单个/批量）、按订阅/状态筛选、按延迟排序
- 测速：真实延迟（经代理访问 generate_204）/ TCP 延迟
- SSRF 防护（DNS 预解析 + 内网地址拒绝 + 不跟随重定向）

### 入站管理
- SOCKS/HTTP 代理端口配置
- 可选的账号密码鉴权
- UDP 支持开关

### 出站管理
- 查看/管理所有节点
- 6 种协议自定义代理：手动填写或粘贴链接，编辑时完整回填；支持链式代理（dialerProxy）
- 节点组：成员可为节点或自定义代理，策略 leastPing/random/roundRobin，按关键字批量选择

### 分流规则
- 支持类型：domain-suffix、full、keyword、geosite、ip、geoip、port；一条规则可填多个值
- 规则值前后端双重校验；出口不可用时标红提示
- 域名策略可选（默认 IPIfNonMatch，保证 geoip 规则对域名请求生效）
- 预设路由模板（中文名，重复套用不会重复添加）
- 未保存修改离开提醒，支持「保存并应用」

### 配置管理
- 事务式应用：校验 → 备份 → 重启 → 存活检查，失败自动回滚并展示 Xray 日志
- Xray 崩溃检测与自动重启（指数退避，连续快速崩溃 5 次后停止）
- 面板内查看 Xray 日志、版本；状态每 10 秒刷新
- Dirty 状态检测；配置提示（不可用出口等）
- 查看当前生效配置与待应用配置预览

### 安全
- PBKDF2-SHA256 密码哈希 + crypto/subtle.ConstantTimeCompare（防时序攻击）
- Bearer Token 会话管理（7 天过期，内存存储）
- 凭据文件 0600 权限
- 订阅拉取 SSRF 防护
- 支持环境变量/随机生成两种密码初始化方式；随机密码同时写入数据目录 `initial-password.txt`，`PasswordRec.Initial` 为真时前端提醒修改
- 忘记密码：`xray-panel passwd [密码]` 只写 `password-reset.json`（哈希），运行中的面板在登录前 / 后台 30 秒周期 / 启动时读取生效并清空会话（不与运行中的面板争写 panel.json）

## API 路由

全部路由注册在 `handler/server.go` 的 `Routes()` 方法中。

鉴权路由（Bearer Token 中间件保护）：
- `/api/auth/*` — 登出、当前用户、修改密码
- `/api/inbounds` — 入站 CRUD
- `/api/proxies` — 自定义代理 CRUD
- `/api/balancers` — 负载均衡 CRUD
- `/api/routing` — 分流规则、模板、出站标签
- `/api/subscriptions` — 订阅 CRUD + 单条/全部拉取
- `/api/nodes` — 节点列表、`PUT /api/nodes/{tag}` 启停、`POST /api/nodes/batch`、`POST /api/nodes/test`（mode=real|tcp）
- `/api/xray/*` — 状态、重启、日志；`/api/apply`、`/api/config`、`/api/config/draft`、`/api/config/diff`、`/api/topology`
- `/api/balancers/status|preview|{tag}/select` — 节点组状态、成员预览、手动切换（立即应用）
- `/api/users`、`/api/users/{name}`、`/api/users/{name}/share` — 用户管理
- `/api/rulesets`、`/api/rulesets/{tag}`、`/api/rulesets/{tag}/refresh|entries` — 域名/IP 分组（订阅分组列表中只返回前 20 条预览）；`/api/geo/catalog?kind=site|ip`、`/api/geo/entries` — 内置分类浏览
- `/api/routing/test` — 规则测试；`/api/traffic` — 流量统计；`/api/online` — 在线用户（q/inbound/sort/offset/limit 服务端筛选分页，summary 为整体统计）与上下线记录（event_kind/event_limit）
- `/api/history`、`/api/history/{id}/diff|restore` — 配置历史；`/api/backup`、`/api/backup/restore` — 备份
- `/api/settings`、`/api/settings/notify/test`、`/api/geo/update` — 设置；`/api/inbounds/{tag}/share`、`/api/tools/gen` — 分享链接与密钥生成

公开路由：
- `POST /api/auth/login` — 登录

SPA fallback：非 API 路径返回 `index.html`。

## 测试

```bash
cd go-backend && go test ./...
```

Go 测试覆盖 app、config、model、auth、store、handler、service；前端 `cd frontend && npm test`（vitest）。
所有接口抽象都有 Fake 实现；`App.SetFetcher` 可替换订阅拉取用于测试。

## 部署

```bash
# 一键安装（systemd，不需要 Docker）：配置 /etc/xray-panel/config.env，数据 /var/lib/xray-panel
curl -fsSL https://raw.githubusercontent.com/Daydayoneup/xray-webbackend/main/install.sh | sudo bash
# Docker：compose 使用 CI 推送的 ghcr.io/daydayoneup/xray-webbackend 镜像，配置见 .env.example
docker compose up -d
# 访问 http://<IP>:2017
```

找不到 Xray 时面板先启动、后台下载官方 Release 到 `$PANEL_DATA_DIR/bin`（geo 数据也在此，`AssetDir` 会回退到 Xray 同目录），顶栏显示进度/失败，可重试（`POST /api/xray/install`）。首次安装时 Xray 未运行，不能经本机代理，需 `GH_PROXY` 或 `HTTPS_PROXY`。

关键环境变量：`TZ`（scratch 镜像已内嵌时区数据）、`PANEL_PORT`、`PANEL_PASSWORD`、`XRAY_BIN`、`PANEL_DATA_DIR`、`SUBSCRIPTION_ALLOW_INTERNAL`、`METRICS_PORT`（默认 10085，0 关闭流量统计）、`XRAY_LOCATION_ASSET`、`GH_PROXY`、`PANEL_CONFIG`。

## 代码规范

- 错误信息使用中文（与前端 UI 语言一致）
- 日志使用 `log/slog`
- JSON 序列化：Auth.Password ↔ JSON key "pass"（与 Python 版兼容）
- 原子文件写入：先写 `.tmp`，再 rename
- 状态迁移在 Load 时完成，兼容旧格式
