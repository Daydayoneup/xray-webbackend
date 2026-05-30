# Xray Panel Go 迁移设计文档

> 将 xray-panel 后端从 Python/FastAPI 迁移至 Golang，保持 HTTP API 契约不变，利用 Go 特性改进内部架构。

## 一、目标与范围

### 目标

- **单二进制部署**：Go 编译产物 + xray-core，无运行时依赖
- **更小镜像**：Docker 镜像从 ~200MB 降至 ~25MB（`scratch` 基础镜像）
- **更低内存**：运行内存从 ~50-100MB 降至 ~10-20MB
- **更快启动**：从秒级降至毫秒级
- **API 兼容**：前端零改动，所有请求路径、请求体、响应体、错误格式保持不变

### 范围

- 重写 `backend/` 下全部 16 个 Python 源文件（~1400 行业务代码）
- 重写对应测试文件（~600 行）
- 更新 Dockerfile（多阶段构建：xray + golang build → scratch）
- docker-compose.yml 保持兼容

### 非范围

- 前端不改动
- 不新增功能，不修改 API
- 不更换数据格式（panel.json / config.json 保持不变）
- 不修改 xray-core 版本或配置方式

---

## 二、技术选型

| 组件 | Python | Go | 理由 |
|---|---|---|---|
| Web 框架 | FastAPI | **chi** + `net/http` | 轻量、idiomatic、与标准库完全兼容 |
| 请求校验 | Pydantic | **go-playground/validator** | struct tag 声明规则，生态最成熟 |
| 环境变量 | pydantic-settings | **caarlos0/env** | 纯 struct tag，无需配置文件 |
| OpenAPI 文档 | FastAPI 自动 | **swaggo/swag** | 注解生成 `/docs` |
| 持久化 | 手写 JSON + threading.Lock | **Store interface** + JSON 文件实现 | 接口抽象，测试时可换内存实现 |
| 密码哈希 | hashlib.pbkdf2_hmac | **golang.org/x/crypto/pbkdf2** | 标准实现 |
| 子进程 | subprocess.Popen | **os/exec** | 标准库 |
| 并发测速 | ThreadPoolExecutor | **goroutines + sync.WaitGroup** | 更轻量，零依赖 |
| 结构化日志 | print() | **log/slog** | Go 1.21+ 标准库 |

### Go 版本

Go 1.22+（利用标准库 `net/http` 的路径参数增强：`r.PathValue("tag")`）

### 外部依赖清单

```
github.com/go-chi/chi/v5         # 路由 + 中间件
github.com/go-playground/validator/v10  # 请求校验
github.com/caarlos0/env/v11       # 环境变量解析
github.com/swaggo/swag            # OpenAPI 生成（开发时依赖）
golang.org/x/crypto               # PBKDF2 密码哈希
```

所有依赖均为 Go 生态中最成熟稳定的选择，无任何框架锁定风险。

---

## 三、项目布局

```
xray-panel-go/
├── cmd/
│   └── server/
│       └── main.go              # 入口：组装、启动、优雅关闭
├── internal/
│   ├── config/
│   │   └── config.go            # Settings struct + 环境变量绑定
│   ├── model/
│   │   ├── state.go             # PanelState + 子模型 (Inbound/Proxy/...)
│   │   └── api.go               # API 请求/响应 struct + validation tag
│   ├── store/
│   │   └── store.go             # Store interface + JSON 文件实现 + 迁移
│   ├── auth/
│   │   ├── password.go          # PBKDF2 哈希/校验
│   │   └── session.go           # 内存 SessionStore
│   ├── handler/
│   │   ├── server.go            # Server struct + Routes() 路由挂载
│   │   ├── helpers.go           # writeJSON / writeError / decodeJSON
│   │   ├── auth.go              # POST /api/auth/login, /logout, PUT /password
│   │   ├── inbounds.go          # CRUD /api/inbounds
│   │   ├── proxies.go           # CRUD /api/proxies
│   │   ├── balancers.go         # CRUD /api/balancers
│   │   ├── routing.go           # /api/routing, /api/routing/templates, /api/outbounds
│   │   ├── subscription.go      # /api/subscription, /api/nodes, /api/nodes/test
│   │   └── xray.go              # /api/xray/status, /api/apply, /api/config, /api/topology
│   ├── middleware/
│   │   └── auth.go              # Bearer token 鉴权中间件 (chi middleware)
│   ├── service/
│   │   ├── xray_proc.go         # XrayProc interface + os/exec 子进程管理
│   │   ├── xray_sub.go          # 订阅链接解析 (vmess/vless/trojan/ss)
│   │   ├── config_builder.go    # 生成 Xray JSON 配置 (纯函数)
│   │   ├── inbounds.go          # 入站校验 + Xray inbound 翻译
│   │   ├── proxies.go           # 代理校验 + Xray outbound 翻译
│   │   └── routing.go           # 规则翻译 + 模板
│   └── app/
│       └── app.go               # App 聚合 (store + sessions + xray)，业务方法
├── Dockerfile                    # 多阶段：xray + go build → scratch
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

### 布局原则

1. **`internal/`** — 整个应用是不可导入的外部程序，`internal` 阻止外部依赖
2. **`cmd/server/`** — 单入口，只做组装和启动，不含业务逻辑
3. **handler 文件按路由前缀拆分** — 与 Python `routers/` 一一对应
4. **model 分离 `state.go` 和 `api.go`** — 持久化模型 vs 请求/响应模型，职责清晰

---

## 四、核心接口设计

### 4.1 Store（持久化层）

```go
type Store interface {
    Load() (*model.PanelState, error)
    Save(state *model.PanelState) error
    Lock()
    Unlock()
}
```

- **JSON 实现**：原子写入（.tmp → os.Rename），启动时自动迁移旧数据
- **测试假实现**：内存 `map`，不写文件
- **Lock/Unlock 暴露**：允许调用方在"读→改→写"三步之间持有锁，与 Python `with app.lock:` 模式一致

### 4.2 XrayProc（子进程管理）

```go
type XrayProc interface {
    Running() bool
    Start(configPath string)
    Stop()
    Restart(configPath string) bool
    TestConfig(cfgJSON []byte) (bool, string)
}
```

- **真实实现**：`os/exec.Cmd`，`SIGTERM` → 5s 超时 → `SIGKILL`
- **测试假实现**：始终返回 running=true, test=ok
- **启动非阻塞**：`cmd.Start()` 不调用 `Wait()`

### 4.3 依赖流向

```
main.go
  ├── config.Settings          ← 环境变量，只读
  ├── store.Store              ← JSON 文件 → *model.PanelState
  ├── auth.SessionStore        ← 内存会话表
  ├── service.XrayProc         ← os/exec 子进程
  └── app.App                  ← 聚合上述三者 + 业务方法
        └── handler.Server     ← HTTP 路由，闭包注入 app
```

### 4.4 无全局单例

Python 用 `_app_state: AppState | None` 全局变量 + `Depends(get_app_state)` 注入。Go 版改为：

- `app.App` 在 `main.go` 构造
- 通过 `handler.Server` 的方法接收者访问
- 测试时直接构造 `handler.Server{App: testApp}`，无需 monkeypatch 环境变量

---

## 五、数据模型

### 5.1 Auth 字段别名（by_alias 处理）

Python `Auth` 模型中 `password` 字段在 JSON 中序列化为 `"pass"`，且 `populate_by_name=True` 允许反序列化同时接受 `"pass"` 和 `"password"`。

Go 实现：自定义 `MarshalJSON` / `UnmarshalJSON`：

```go
type Auth struct {
    User     string `json:"user"`
    Password string `json:"-"`  // 不参与默认序列化
}

func (a Auth) MarshalJSON() ([]byte, error) {
    return json.Marshal(&struct {
        User string `json:"user"`
        Pass string `json:"pass"`
    }{a.User, a.Password})
}

func (a *Auth) UnmarshalJSON(b []byte) error {
    var raw struct {
        User     string `json:"user"`
        Pass     string `json:"pass"`
        Password string `json:"password"`
    }
    json.Unmarshal(b, &raw)
    a.User = raw.User
    a.Password = raw.Pass
    if a.Password == "" {
        a.Password = raw.Password // populate_by_name 兼容
    }
    return nil
}
```

### 5.2 Node.Outbound — 保留 map[string]any

Xray 配置结构因协议（vmess/vless/trojan/ss）而异，嵌套层级深且不规则。`Node.Outbound` 保留 `map[string]any` 保持灵活性，`config_builder` 只做 `deepCopy` + 追加 `tag` 字段，不解析内部结构。

### 5.3 其余模型：标准 struct tag

Inbound、Proxy、Balancer、Rule、Subscription 等均为平铺结构，JSON 键名与 Go 字段名一致，标准 `json:"..."` tag 即可。

---

## 六、Handler 通用模式

### 6.1 路由挂载

```go
func (s *Server) Routes() chi.Router {
    r := chi.NewRouter()
    // 公开
    r.Post("/api/auth/login", s.Login)
    // 受保护
    r.Group(func(r chi.Router) {
        r.Use(middleware.RequireAuth(s.App.Sessions()))
        r.Get("/api/inbounds", s.ListInbounds)
        r.Post("/api/inbounds", s.CreateInbound)
        // ...
    })
    return r
}
```

### 6.2 请求处理模板

```go
func (s *Server) CreateInbound(w http.ResponseWriter, r *http.Request) {
    var body model.InboundIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    result, err := s.App.CreateInbound(body)
    if err != nil {
        writeError(w, 400, err.Error()); return
    }
    writeJSON(w, 201, result)
}
```

### 6.3 响应格式

错误响应格式与 FastAPI 完全一致：

```json
{"detail": "入站端口 2017 与面板端口冲突"}
```

成功响应直接返回 JSON 对象或数组，201 创建场景使用 201 状态码。

---

## 七、订阅解析模块

### 7.1 模块接口

```go
func ExtractLinks(content string) (links []string, meta map[string]string)
func ParseLinks(links []string) (nodes []NodeRaw, skipped []SkipInfo)
func AssignTags(nodes []NodeRaw)
func MeasureLatency(nodes []NodeRaw)
```

### 7.2 协议分发

```go
type parserFunc func(link string) (NodeRaw, error)

var parsers = map[string]parserFunc{
    "vmess": parseVMess,
    "vless": parseVLess,
    "trojan": parseTrojan,
    "ss":    parseSS,
}
```

### 7.3 并发测速

```go
func MeasureLatency(nodes []NodeRaw) {
    sem := make(chan struct{}, 32)  // 最多 32 并发
    var wg sync.WaitGroup
    for i := range nodes {
        wg.Add(1)
        go func(n *NodeRaw) {
            defer wg.Done()
            sem <- struct{}{}; defer func() { <-sem }()
            n.Latency = tcpPing(n.Host, n.Port)
        }(&nodes[i])
    }
    wg.Wait()
}
```

Python `ThreadPoolExecutor(max_workers=32)` → Go goroutine + 信号量 channel，零外部依赖。

### 7.4 纯标准库

`xray_sub.go` 只用 `encoding/base64`、`net/url`、`encoding/json`、`net`、`sync`、`time` — 全部来自 Go 标准库。预计 ~350 行，是项目最大单文件。

---

## 八、错误处理策略

| 场景 | 处理方式 |
|---|---|
| 请求格式错误（JSON 解析失败） | HTTP 400 + `{"detail": "请求格式错误"}` |
| 字段校验失败（validator） | HTTP 400 + 中文错误描述 |
| 业务规则冲突（端口重复等） | HTTP 400 + 业务错误描述 |
| 资源不存在 | HTTP 404 |
| 鉴权失败 | HTTP 401 + `{"detail": "未授权或登录已过期"}` |
| 订阅拉取失败 | HTTP 502 + `{"detail": "拉取失败: ..."}` |
| 配置校验失败（xray -test） | HTTP 400 + 末尾 800 字符输出 |
| 内部错误（文件读写失败等） | HTTP 500（仅返回通用错误，详情记日志） |

**原则**：handler 层不 panic，所有错误走 `writeError`。仅 `main.go` 的初始化阶段允许 `log.Fatal` / `os.Exit`。

---

## 九、测试策略

### 9.1 单元测试

| 层 | 策略 |
|---|---|
| **model** | 测试 JSON 序列化/反序列化，特别是 Auth 的 alias 行为 |
| **service** | 纯函数直接测试（config_builder, routing, xray_sub） |
| **store** | 用 `t.TempDir()` 创建临时文件，测试迁移逻辑和原子写入 |
| **auth** | 测试密码哈希/校验、session 创建/过期/吊销 |

### 9.2 集成测试

```go
func TestCreateInbound(t *testing.T) {
    store := store.NewMemoryStore()         // 假实现
    xray := &service.FakeXray{Alive: true}  // 假实现
    app := app.New(app.Config{Store: store, XrayProc: xray, ...})
    srv := handler.NewServer(app)
    ts := httptest.NewServer(srv.Routes())   // 标准库 httptest
    defer ts.Close()

    // 与 Python TestClient 同样用法
    resp, _ := http.Post(ts.URL + "/api/inbounds", "application/json", body)
    assert.Equal(t, 201, resp.StatusCode)
}
```

### 9.3 对比 Python 测试

| Python | Go |
|---|---|
| `pytest` + `TestClient` | `testing` + `httptest.NewServer` |
| `tmp_path` fixture 隔离文件 | `t.TempDir()` |
| `monkeypatch.setenv` 注入配置 | 直接构造 `config.Settings{}` |
| `XRAY_BIN=/bin/true` | `FakeXray` struct |
| `client.post("/api/inbounds", json=...)` | `http.Post(ts.URL + "/api/inbounds", ...)` |

---

## 十、Docker 部署

### 10.1 多阶段构建

```dockerfile
# 阶段 1：xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:latest AS xray

# 阶段 2：Go 构建（静态链接）
FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o xray-panel ./cmd/server

# 阶段 3：极简运行时
FROM scratch
COPY --from=xray /usr/local/bin/xray    /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
COPY --from=builder /build/xray-panel   /xray-panel
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
ENV PANEL_LISTEN=0.0.0.0
ENV PANEL_PORT=2017
ENV PANEL_DATA_DIR=/data/xray
EXPOSE 2017 10808 10809
ENTRYPOINT ["/xray-panel"]
```

### 10.2 与环境变量兼容

环境变量名与 Python 版完全一致：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PANEL_PORT` | `2017` | 面板端口 |
| `PANEL_LISTEN` | `0.0.0.0` | 监听地址 |
| `PANEL_DATA_DIR` | `/data/xray` | 数据目录 |
| `XRAY_BIN` | `/usr/local/bin/xray` | xray 路径 |
| `PANEL_PASSWORD` | 随机生成 | 登录密码 |
| `SOCKS_PORT` | `10808` | SOCKS 入站端口 |
| `HTTP_PORT` | `10809` | HTTP 入站端口 |
| `SUBSCRIPTION_ALLOW_INTERNAL` | `false` | 放行内网订阅 |

---

## 十一、预估代码量

| 模块 | 预估 Go 行数 |
|---|---|
| config | ~50 |
| model (state + api) | ~200 |
| store (JSON + 迁移) | ~150 |
| auth (password + session) | ~120 |
| handler (7 个路由文件 + server + helpers) | ~500 |
| middleware (auth) | ~30 |
| service/xray_proc | ~100 |
| service/xray_sub | ~350 |
| service/config_builder | ~120 |
| service/inbounds + proxies + routing | ~150 |
| app | ~130 |
| main.go | ~80 |
| **业务代码合计** | **~2000** |
| 单元测试 | ~800 |
| **总计** | **~2800** |

---

## 十二、风险与注意事项

1. **Auth alias 序列化** — 自定义 MarshalJSON/UnmarshalJSON 需要充分的单元测试覆盖，确保与 Python `by_alias=True` 行为完全一致
2. **Node.Outbound 用 map[string]any** — 牺牲类型安全换灵活性，config_builder 只做 shallow copy + 追加 tag，不操作内部字段，风险可控
3. **并发安全** — Python 用单一 `threading.Lock`，Go 用 `sync.Mutex`。注意锁的粒度不能变（跨读-改-写），否则引入竞态
4. **优雅关闭** — Python 的 lifespan yield 换成 `signal.NotifyContext` + `http.Server.Shutdown`，需验证 xray 子进程能被正确终止
5. **OpenAPI 文档** — swaggo 需要手动注解每个 handler，不如 FastAPI 自动生成方便，但不影响运行时功能
6. **scratch 镜像限制** — `FROM scratch` 不含 shell/CA 证书，订阅拉取 HTTPS 时需 `COPY --from=builder /etc/ssl/certs/ca-certificates.crt`
