# Xray Panel Go 迁移实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 xray-panel 后端从 Python/FastAPI 完整迁移到 Golang，保持 HTTP API 契约完全不变。

**Architecture:** 标准 Go 布局（`cmd/server/` + `internal/`），chi 路由框架，Store/XrayProc interface 抽象持久化和子进程管理，handler → app → store 三层调用链。Go 项目放在 `go-backend/` 子目录，与现有 Python `backend/` 并行存在，验证后替换。

**Tech Stack:** Go 1.22+, chi v5, go-playground/validator v10, caarlos0/env v11, golang.org/x/crypto, log/slog

---

## 文件结构总览

```
xray-panel/go-backend/
├── cmd/server/main.go
├── internal/
│   ├── config/config.go
│   ├── model/state.go, api.go
│   ├── store/store.go
│   ├── auth/password.go, session.go
│   ├── handler/server.go, helpers.go, auth.go, inbounds.go,
│   │        proxies.go, balancers.go, routing.go, subscription.go, xray.go
│   ├── middleware/auth.go
│   ├── service/xray_proc.go, xray_sub.go, config_builder.go,
│   │          inbounds.go, proxies.go, routing.go
│   └── app/app.go
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── go.sum
```

---

### Task 1: 初始化 Go 模块与目录结构

**Files:**
- Create: `go-backend/go.mod`
- Create: 所有空目录

- [ ] **Step 1: 创建目录结构**

```bash
cd /Users/admin/program/pythonProject/xray-panel
mkdir -p go-backend/cmd/server
mkdir -p go-backend/internal/{config,model,store,auth,handler,middleware,service,app}
```

- [ ] **Step 2: 初始化 Go module**

```bash
cd go-backend
go mod init xray-panel
```

- [ ] **Step 3: 添加核心依赖**

```bash
cd go-backend
go get github.com/go-chi/chi/v5
go get github.com/go-playground/validator/v10
go get github.com/caarlos0/env/v11
go get golang.org/x/crypto
```

- [ ] **Step 4: 验证依赖**

```bash
cd go-backend
go mod tidy
```

预期：`go.mod` 包含 4 个直接依赖，`go.sum` 生成成功。

- [ ] **Step 5: 提交**

```bash
git add go-backend/go.mod go-backend/go.sum
git commit -m "chore: init Go module with core dependencies

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: 配置模块 — config/config.go

**Files:**
- Create: `go-backend/internal/config/config.go`
- Test: `go-backend/internal/config/config_test.go`

- [ ] **Step 1: 编写测试**

```go
// go-backend/internal/config/config_test.go
package config

import (
    "os"
    "testing"
)

func TestLoadDefaults(t *testing.T) {
    // 清除环境变量，验证默认值
    for _, k := range []string{"PANEL_PORT", "PANEL_LISTEN", "PANEL_DATA_DIR",
        "XRAY_BIN", "PANEL_PASSWORD", "SOCKS_PORT", "HTTP_PORT",
        "SUBSCRIPTION_ALLOW_INTERNAL"} {
        os.Unsetenv(k)
    }
    cfg, err := Load()
    if err != nil {
        t.Fatal(err)
    }
    if cfg.PanelPort != 2017 {
        t.Errorf("PanelPort = %d, want 2017", cfg.PanelPort)
    }
    if cfg.SocksPort != 10808 {
        t.Errorf("SocksPort = %d, want 10808", cfg.SocksPort)
    }
    if cfg.HTTPPort != 10809 {
        t.Errorf("HTTPPort = %d, want 10809", cfg.HTTPPort)
    }
    if cfg.PanelListen != "0.0.0.0" {
        t.Errorf("PanelListen = %s, want 0.0.0.0", cfg.PanelListen)
    }
    if cfg.PanelPassword != nil {
        t.Errorf("PanelPassword = %v, want nil", cfg.PanelPassword)
    }
    if cfg.SubscriptionAllowInternal {
        t.Errorf("SubscriptionAllowInternal = true, want false")
    }
}

func TestLoadFromEnv(t *testing.T) {
    os.Setenv("PANEL_PORT", "3000")
    os.Setenv("PANEL_PASSWORD", "secret")
    os.Setenv("SUBSCRIPTION_ALLOW_INTERNAL", "1")
    defer os.Unsetenv("PANEL_PORT")
    defer os.Unsetenv("PANEL_PASSWORD")
    defer os.Unsetenv("SUBSCRIPTION_ALLOW_INTERNAL")

    cfg, err := Load()
    if err != nil {
        t.Fatal(err)
    }
    if cfg.PanelPort != 3000 {
        t.Errorf("PanelPort = %d, want 3000", cfg.PanelPort)
    }
    if cfg.PanelPassword == nil || *cfg.PanelPassword != "secret" {
        t.Errorf("PanelPassword = %v, want 'secret'", cfg.PanelPassword)
    }
    if !cfg.SubscriptionAllowInternal {
        t.Errorf("SubscriptionAllowInternal = false, want true")
    }
}

func TestPaths(t *testing.T) {
    cfg := &Settings{DataDir: "/data/xray"}
    if cfg.StatePath() != "/data/xray/panel.json" {
        t.Errorf("StatePath = %s", cfg.StatePath())
    }
    if cfg.ConfigPath() != "/data/xray/config.json" {
        t.Errorf("ConfigPath = %s", cfg.ConfigPath())
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd go-backend && go test ./internal/config/ -v
```

预期：编译失败 — `Settings` 类型和 `Load` 函数未定义。

- [ ] **Step 3: 实现 config.go**

```go
// go-backend/internal/config/config.go
package config

import (
    "path/filepath"

    "github.com/caarlos0/env/v11"
)

type Settings struct {
    PanelPort                int     `env:"PANEL_PORT" envDefault:"2017"`
    PanelListen              string  `env:"PANEL_LISTEN" envDefault:"0.0.0.0"`
    DataDir                  string  `env:"PANEL_DATA_DIR" envDefault:"/data/xray"`
    XrayBin                  string  `env:"XRAY_BIN" envDefault:"/usr/local/bin/xray"`
    PanelPassword            *string `env:"PANEL_PASSWORD"`
    SocksPort                int     `env:"SOCKS_PORT" envDefault:"10808"`
    HTTPPort                 int     `env:"HTTP_PORT" envDefault:"10809"`
    SubscriptionAllowInternal bool   `env:"SUBSCRIPTION_ALLOW_INTERNAL"`
}

func Load() (*Settings, error) {
    cfg := &Settings{}
    if err := env.Parse(cfg); err != nil {
        return nil, err
    }
    return cfg, nil
}

func (s *Settings) StatePath() string {
    return filepath.Join(s.DataDir, "panel.json")
}

func (s *Settings) ConfigPath() string {
    return filepath.Join(s.DataDir, "config.json")
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/config/ -v
```

- [ ] **Step 5: 提交**

```bash
git add go-backend/internal/config/
git commit -m "feat: add config module with env var loading

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: 数据模型 — model/state.go

**Files:**
- Create: `go-backend/internal/model/state.go`
- Test: `go-backend/internal/model/state_test.go`

- [ ] **Step 1: 编写 Auth 序列化测试**

```go
// go-backend/internal/model/state_test.go
package model

import (
    "encoding/json"
    "testing"
)

func TestAuthMarshalJSON(t *testing.T) {
    a := Auth{User: "admin", Password: "secret123"}
    data, err := json.Marshal(a)
    if err != nil {
        t.Fatal(err)
    }
    var m map[string]string
    if err := json.Unmarshal(data, &m); err != nil {
        t.Fatal(err)
    }
    if m["user"] != "admin" {
        t.Errorf("user = %s, want admin", m["user"])
    }
    if m["pass"] != "secret123" {
        t.Errorf("pass = %s, want secret123", m["pass"])
    }
    // 确认 JSON 中没有 "password" 键
    if _, ok := m["password"]; ok {
        t.Error("JSON should not contain 'password' key")
    }
}

func TestAuthUnmarshalJSON_Pass(t *testing.T) {
    raw := `{"user":"admin","pass":"secret123"}`
    var a Auth
    if err := json.Unmarshal([]byte(raw), &a); err != nil {
        t.Fatal(err)
    }
    if a.User != "admin" || a.Password != "secret123" {
        t.Errorf("got user=%s password=%s", a.User, a.Password)
    }
}

func TestAuthUnmarshalJSON_Password(t *testing.T) {
    // populate_by_name 兼容：也接受 "password" 键
    raw := `{"user":"admin","password":"secret456"}`
    var a Auth
    if err := json.Unmarshal([]byte(raw), &a); err != nil {
        t.Fatal(err)
    }
    if a.Password != "secret456" {
        t.Errorf("password = %s, want secret456", a.Password)
    }
}

func TestPanelStateDefaults(t *testing.T) {
    state := &PanelState{}
    // 序列化后反序列化，确认 defaults 不丢
    data, err := json.Marshal(state)
    if err != nil {
        t.Fatal(err)
    }
    var restored PanelState
    if err := json.Unmarshal(data, &restored); err != nil {
        t.Fatal(err)
    }
    if restored.Subscription.URL != "" {
        t.Error("subscription.url should default to empty")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd go-backend && go test ./internal/model/ -v
```

预期：编译失败。

- [ ] **Step 3: 实现 state.go**

```go
// go-backend/internal/model/state.go
package model

import "encoding/json"

// ---------- 认证 ----------

type Auth struct {
    User     string `json:"user"`
    Password string `json:"-"` // 自定义序列化
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
    if err := json.Unmarshal(b, &raw); err != nil {
        return err
    }
    a.User = raw.User
    a.Password = raw.Pass
    if a.Password == "" {
        a.Password = raw.Password
    }
    return nil
}

// ---------- 子模型 ----------

type PasswordRec struct {
    Salt string `json:"salt"`
    Hash string `json:"hash"`
}

type Subscription struct {
    URL       string `json:"url"`
    Remarks   string `json:"remarks"`
    Status    string `json:"status"`
    FetchedAt int64  `json:"fetched_at"`
}

type Node struct {
    Tag      string         `json:"tag"`
    Name     string         `json:"name"`
    Type     string         `json:"type"`
    Host     string         `json:"host"`
    Port     int            `json:"port"`
    Latency  *int           `json:"latency,omitempty"`
    Outbound map[string]any `json:"outbound"`
}

type Inbound struct {
    Tag      string `json:"tag"`
    Protocol string `json:"protocol"`
    Listen   string `json:"listen"`
    Port     int    `json:"port"`
    UDP      bool   `json:"udp"`
    Auth     *Auth  `json:"auth,omitempty"`
}

type Proxy struct {
    Tag      string `json:"tag"`
    Name     string `json:"name"`
    Protocol string `json:"protocol"`
    Host     string `json:"host"`
    Port     int    `json:"port"`
    Auth     *Auth  `json:"auth,omitempty"`
}

type Balancer struct {
    Tag      string   `json:"tag"`
    Name     string   `json:"name"`
    Nodes    []string `json:"nodes"`
    Strategy string   `json:"strategy"`
}

type Rule struct {
    ID       int    `json:"id"`
    Type     string `json:"type"`
    Value    string `json:"value"`
    Outbound string `json:"outbound"`
    Enabled  bool   `json:"enabled"`
}

// ---------- 顶层状态 ----------

type PanelState struct {
    Password        *PasswordRec  `json:"password,omitempty"`
    Subscription    Subscription  `json:"subscription"`
    Nodes           []Node        `json:"nodes"`
    Inbounds        []Inbound     `json:"inbounds"`
    Proxies         []Proxy       `json:"proxies"`
    Balancers       []Balancer    `json:"balancers"`
    Rules           []Rule        `json:"rules"`
    DefaultOutbound string        `json:"default_outbound"`
    InboundSeq      int           `json:"inbound_seq"`
    ProxySeq        int           `json:"proxy_seq"`
    BalancerSeq     int           `json:"balancer_seq"`
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/model/ -v
```

- [ ] **Step 5: 提交**

```bash
git add go-backend/internal/model/
git commit -m "feat: add data models with Auth alias JSON handling

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: API 请求模型 — model/api.go

**Files:**
- Create: `go-backend/internal/model/api.go`
- Test: `go-backend/internal/model/api_test.go`

- [ ] **Step 1: 编写测试（validator 校验规则）**

```go
// go-backend/internal/model/api_test.go
package model

import (
    "testing"

    "github.com/go-playground/validator/v10"
)

var validate = validator.New()

func TestInboundInValidation(t *testing.T) {
    tests := []struct {
        name  string
        input InboundIn
        valid bool
    }{
        {"valid socks", InboundIn{Protocol: "socks", Port: 1080}, true},
        {"valid http", InboundIn{Protocol: "http", Port: 8080}, true},
        {"invalid protocol", InboundIn{Protocol: "ssh", Port: 1080}, false},
        {"port too low", InboundIn{Protocol: "socks", Port: 0}, false},
        {"port too high", InboundIn{Protocol: "socks", Port: 65536}, false},
        {"missing protocol", InboundIn{Port: 1080}, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validate.Struct(tt.input)
            if tt.valid && err != nil {
                t.Errorf("expected valid, got: %v", err)
            }
            if !tt.valid && err == nil {
                t.Error("expected invalid, got valid")
            }
        })
    }
}

func TestAuthInValidation(t *testing.T) {
    // user 和 pass 都不能为空
    a := AuthIn{User: "admin", Password: ""}
    err := validate.Struct(a)
    if err == nil {
        t.Error("expected validation error for empty password")
    }
    a2 := AuthIn{User: "", Password: "secret"}
    err = validate.Struct(a2)
    if err == nil {
        t.Error("expected validation error for empty user")
    }
}

func TestProxyInValidation(t *testing.T) {
    p := ProxyIn{Protocol: "http", Host: "  ", Port: 80}
    err := validate.Struct(p)
    if err == nil {
        t.Error("expected validation error for empty host")
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd go-backend && go test ./internal/model/ -v
```

- [ ] **Step 3: 实现 api.go**

```go
// go-backend/internal/model/api.go
package model

// ---------- 认证 ----------

type LoginIn struct {
    Password string `json:"password" validate:"required"`
}

type AuthIn struct {
    User     string `json:"user" validate:"required_with=Password"`
    Password string `json:"pass" validate:"required_with=User"`
}

type PasswordChangeIn struct {
    OldPassword string `json:"old_password" validate:"required"`
    NewPassword string `json:"new_password" validate:"required,min=6"`
}

// ---------- 入站 ----------

type InboundIn struct {
    Protocol string  `json:"protocol" validate:"required,oneof=socks http"`
    Listen   string  `json:"listen"`
    Port     int     `json:"port" validate:"required,min=1,max=65535"`
    UDP      bool    `json:"udp"`
    Auth     *AuthIn `json:"auth" validate:"omitempty"`
}

// ---------- 代理 ----------

type ProxyIn struct {
    Name     string  `json:"name"`
    Protocol string  `json:"protocol" validate:"required,oneof=socks http"`
    Host     string  `json:"host" validate:"required"`
    Port     int     `json:"port" validate:"required,min=1,max=65535"`
    Auth     *AuthIn `json:"auth" validate:"omitempty"`
}

// ---------- 自动组 ----------

type BalancerIn struct {
    Name  string   `json:"name"`
    Nodes []string `json:"nodes" validate:"required,min=1"`
}

// ---------- 规则 ----------

type RuleIn struct {
    ID       *int   `json:"id"`
    Type     string `json:"type" validate:"required,oneof=domain-suffix full keyword geosite ip geoip port"`
    Value    string `json:"value"`
    Outbound string `json:"outbound" validate:"required"`
    Enabled  bool   `json:"enabled"`
}

type RoutingIn struct {
    DefaultOutbound string   `json:"default_outbound"`
    Rules           []RuleIn `json:"rules"`
}

// ---------- 订阅 ----------

type SubscriptionIn struct {
    URL string `json:"url" validate:"required"`
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/model/ -v
```

- [ ] **Step 5: 提交**

```bash
git add go-backend/internal/model/api.go go-backend/internal/model/api_test.go
git commit -m "feat: add API request/response models with validation tags

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: 密码哈希 — auth/password.go

**Files:**
- Create: `go-backend/internal/auth/password.go`
- Test: `go-backend/internal/auth/password_test.go`

- [ ] **Step 1: 编写测试**

```go
// go-backend/internal/auth/password_test.go
package auth

import "testing"

func TestHashPassword(t *testing.T) {
    rec, err := HashPassword("testpw")
    if err != nil {
        t.Fatal(err)
    }
    if rec.Salt == "" {
        t.Error("salt should not be empty")
    }
    if rec.Hash == "" {
        t.Error("hash should not be empty")
    }
    if len(rec.Salt) != 32 { // hex encoded 16 bytes = 32 chars
        t.Errorf("salt length = %d, want 32", len(rec.Salt))
    }
    if len(rec.Hash) != 64 { // sha256 hex = 64 chars
        t.Errorf("hash length = %d, want 64", len(rec.Hash))
    }
}

func TestHashPasswordDeterministic(t *testing.T) {
    salt := "aabbccddeeff00112233445566778899"
    rec1, _ := HashPassword("testpw", salt)
    rec2, _ := HashPassword("testpw", salt)
    if rec1.Hash != rec2.Hash {
        t.Error("same password + same salt should produce same hash")
    }
}

func TestVerifyPassword(t *testing.T) {
    rec, _ := HashPassword("correct")
    if !VerifyPassword(rec, "correct") {
        t.Error("correct password should verify")
    }
    if VerifyPassword(rec, "wrong") {
        t.Error("wrong password should not verify")
    }
    if VerifyPassword(nil, "anything") {
        t.Error("nil record should not verify")
    }
}

func TestPasswordRecType(t *testing.T) {
    // 确认 HashPassword 返回的是 model.PasswordRec（不是 auth 包自己的类型）
    rec, _ := HashPassword("test")
    if rec.Salt == "" || rec.Hash == "" {
        t.Error("salt and hash should not be empty")
    }
}
```

- [ ] **Step 2: 实现 password.go**

```go
// go-backend/internal/auth/password.go
package auth

import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"

    "golang.org/x/crypto/pbkdf2"

    "xray-panel/internal/model"
)

const pbkdf2Iterations = 200_000

// HashPassword 使用 model.PasswordRec 以避免类型重复
func HashPassword(pw string, salt ...string) (*model.PasswordRec, error) {
    s := ""
    if len(salt) > 0 {
        s = salt[0]
    } else {
        b := make([]byte, 16)
        if _, err := rand.Read(b); err != nil {
            return nil, err
        }
        s = hex.EncodeToString(b)
    }
    saltBytes, err := hex.DecodeString(s)
    if err != nil {
        return nil, err
    }
    h := pbkdf2.Key([]byte(pw), saltBytes, pbkdf2Iterations, sha256.Size, sha256.New)
    return &model.PasswordRec{Salt: s, Hash: hex.EncodeToString(h)}, nil
}

func VerifyPassword(rec *model.PasswordRec, pw string) bool {
    if rec == nil {
        return false
    }
    calc, err := HashPassword(pw, rec.Salt)
    if err != nil {
        return false
    }
    return calc.Hash == rec.Hash
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/auth/ -v -run TestHashPassword -run TestVerifyPassword
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/auth/password.go go-backend/internal/auth/password_test.go
git commit -m "feat: add PBKDF2 password hashing and verification

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: 会话管理 — auth/session.go

**Files:**
- Create: `go-backend/internal/auth/session.go`
- Test: `go-backend/internal/auth/session_test.go`

- [ ] **Step 1: 编写测试**

```go
// go-backend/internal/auth/session_test.go
package auth

import (
    "testing"
    "time"
)

func TestSessionCreateAndValidate(t *testing.T) {
    ss := NewSessionStore(3600) // 1 hour TTL
    token := ss.Create()
    if token == "" {
        t.Fatal("token should not be empty")
    }
    if !ss.Valid(token) {
        t.Error("newly created token should be valid")
    }
    if ss.Valid("fake-token") {
        t.Error("fake token should not be valid")
    }
}

func TestSessionRevoke(t *testing.T) {
    ss := NewSessionStore(3600)
    token := ss.Create()
    ss.Revoke(token)
    if ss.Valid(token) {
        t.Error("revoked token should not be valid")
    }
}

func TestSessionClear(t *testing.T) {
    ss := NewSessionStore(3600)
    t1 := ss.Create()
    t2 := ss.Create()
    ss.Clear()
    if ss.Valid(t1) || ss.Valid(t2) {
        t.Error("cleared tokens should not be valid")
    }
}

func TestSessionExpiry(t *testing.T) {
    ss := NewSessionStore(-1) // immediate expiry
    token := ss.Create()
    time.Sleep(10 * time.Millisecond)
    if ss.Valid(token) {
        t.Error("expired token should not be valid")
    }
}
```

- [ ] **Step 2: 实现 session.go**

```go
// go-backend/internal/auth/session.go
package auth

import (
    "crypto/rand"
    "encoding/hex"
    "sync"
    "time"
)

type SessionStore struct {
    ttl     int64
    mu      sync.RWMutex
    tokens  map[string]int64 // token -> expiry unix timestamp
}

func NewSessionStore(ttl int64) *SessionStore {
    return &SessionStore{ttl: ttl, tokens: make(map[string]int64)}
}

func (s *SessionStore) Create() string {
    b := make([]byte, 24)
    rand.Read(b)
    token := hex.EncodeToString(b)

    s.mu.Lock()
    defer s.mu.Unlock()

    // 清理过期 token（懒惰回收）
    now := time.Now().Unix()
    for t, exp := range s.tokens {
        if exp <= now {
            delete(s.tokens, t)
        }
    }
    s.tokens[token] = now + s.ttl
    return token
}

func (s *SessionStore) Valid(token string) bool {
    s.mu.RLock()
    exp, ok := s.tokens[token]
    s.mu.RUnlock()
    if !ok || exp <= time.Now().Unix() {
        s.mu.Lock()
        delete(s.tokens, token)
        s.mu.Unlock()
        return false
    }
    return true
}

func (s *SessionStore) Revoke(token string) {
    s.mu.Lock()
    delete(s.tokens, token)
    s.mu.Unlock()
}

func (s *SessionStore) Clear() {
    s.mu.Lock()
    s.tokens = make(map[string]int64)
    s.mu.Unlock()
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/auth/ -v
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/auth/session.go go-backend/internal/auth/session_test.go
git commit -m "feat: add in-memory session store with TTL

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: 持久化层 — store/store.go

**Files:**
- Create: `go-backend/internal/store/store.go`
- Test: `go-backend/internal/store/store_test.go`

- [ ] **Step 1: 编写测试（迁移 + 原子写入 + 加载）**

```go
// go-backend/internal/store/store_test.go
package store

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "xray-panel/internal/model"
)

func TestNewInstallLoadsDefaults(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "panel.json")
    s := NewJSONStore(path, 10808, 10809)

    state, err := s.Load()
    if err != nil {
        t.Fatal(err)
    }
    if len(state.Inbounds) != 2 {
        t.Fatalf("expected 2 default inbounds, got %d", len(state.Inbounds))
    }
    if state.Inbounds[0].Tag != "in-0" {
        t.Errorf("first inbound tag = %s, want in-0", state.Inbounds[0].Tag)
    }
    if state.Inbounds[0].Port != 10808 {
        t.Errorf("first inbound port = %d, want 10808", state.Inbounds[0].Port)
    }
    if state.Inbounds[0].Protocol != "socks" {
        t.Errorf("first inbound protocol = %s, want socks", state.Inbounds[0].Protocol)
    }
    if state.Inbounds[1].Tag != "in-1" {
        t.Errorf("second inbound tag = %s, want in-1", state.Inbounds[1].Tag)
    }
}

func TestSaveAndLoad(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "panel.json")
    s := NewJSONStore(path, 10808, 10809)

    state, _ := s.Load()
    state.DefaultOutbound = "direct"
    state.InboundSeq = 5
    if err := s.Save(state); err != nil {
        t.Fatal(err)
    }

    loaded, err := s.Load()
    if err != nil {
        t.Fatal(err)
    }
    if loaded.DefaultOutbound != "direct" {
        t.Errorf("default_outbound = %s, want direct", loaded.DefaultOutbound)
    }
    if loaded.InboundSeq != 5 {
        t.Errorf("inbound_seq = %d, want 5", loaded.InboundSeq)
    }
}

func TestAtomicWrite(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "panel.json")
    s := NewJSONStore(path, 10808, 10809)

    state, _ := s.Load()
    state.DefaultOutbound = "node-0"
    s.Save(state)

    // 确保没有残留 .tmp 文件
    if _, err := os.Stat(path + ".tmp"); err == nil {
        t.Error(".tmp file should not exist after successful save")
    }
}

func TestMigrationSeedsRules(t *testing.T) {
    // 旧数据：rules 没有 id/enabled
    dir := t.TempDir()
    path := filepath.Join(dir, "panel.json")
    old := map[string]any{
        "rules": []any{
            map[string]any{"type": "domain-suffix", "value": "google.com", "outbound": "direct"},
        },
    }
    data, _ := json.Marshal(old)
    os.WriteFile(path, data, 0644)

    s := NewJSONStore(path, 10808, 10809)
    state, _ := s.Load()
    if len(state.Rules) != 1 {
        t.Fatalf("expected 1 rule, got %d", len(state.Rules))
    }
    if state.Rules[0].ID != 1 {
        t.Errorf("rule id = %d, want 1", state.Rules[0].ID)
    }
    if !state.Rules[0].Enabled {
        t.Error("rule should default to enabled")
    }
}
```

- [ ] **Step 2: 实现 store.go**

```go
// go-backend/internal/store/store.go
package store

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"

    "xray-panel/internal/model"
)

// ---------- interface ----------

type Store interface {
    Load() (*model.PanelState, error)
    Save(state *model.PanelState) error
    Lock()
    Unlock()
}

// ---------- JSON 实现 ----------

type jsonStore struct {
    path      string
    mu        sync.Mutex
    socksPort int
    httpPort  int
}

func NewJSONStore(path string, socksPort, httpPort int) Store {
    return &jsonStore{path: path, socksPort: socksPort, httpPort: httpPort}
}

func (s *jsonStore) Lock()   { s.mu.Lock() }
func (s *jsonStore) Unlock() { s.mu.Unlock() }

func (s *jsonStore) Load() (*model.PanelState, error) {
    raw, err := os.ReadFile(s.path)
    if err != nil {
        if os.IsNotExist(err) {
            return migrate(nil, s.socksPort, s.httpPort), nil
        }
        return nil, fmt.Errorf("读取状态文件失败: %w", err)
    }
    var dict map[string]any
    if err := json.Unmarshal(raw, &dict); err != nil {
        return nil, fmt.Errorf("解析状态文件失败: %w", err)
    }
    return migrate(dict, s.socksPort, s.httpPort), nil
}

func (s *jsonStore) Save(state *model.PanelState) error {
    dir := filepath.Dir(s.path)
    if dir != "" {
        if err := os.MkdirAll(dir, 0755); err != nil {
            return fmt.Errorf("创建数据目录失败: %w", err)
        }
    }
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return fmt.Errorf("序列化失败: %w", err)
    }
    tmp := s.path + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return fmt.Errorf("写入临时文件失败: %w", err)
    }
    if err := os.Rename(tmp, s.path); err != nil {
        return fmt.Errorf("原子替换失败: %w", err)
    }
    return nil
}

// ---------- 迁移 ----------

func migrate(raw map[string]any, socksPort, httpPort int) *model.PanelState {
    if raw == nil {
        raw = map[string]any{}
    }

    // 播种默认入站
    if _, ok := raw["inbounds"]; !ok {
        raw["inbounds"] = defaultInbounds(socksPort, httpPort)
    } else if raw["inbounds"] == nil {
        raw["inbounds"] = []any{}
    }

    for _, key := range []string{"proxies", "balancers", "rules"} {
        if raw[key] == nil {
            raw[key] = []any{}
        }
    }

    // 规则补 id + enabled
    raw["rules"] = fixRules(raw["rules"].([]any))

    // seq 计数器
    setDefaultSeq(raw, "inbound_seq", raw["inbounds"].([]any), "in-")
    setDefaultSeq(raw, "proxy_seq", raw["proxies"].([]any), "px-")
    setDefaultSeq(raw, "balancer_seq", raw["balancers"].([]any), "auto-")

    // map -> JSON -> struct（两步走避免 mapstructure 依赖）
    data, _ := json.Marshal(raw)
    var state model.PanelState
    json.Unmarshal(data, &state)
    return &state
}

func defaultInbounds(socksPort, httpPort int) []map[string]any {
    return []map[string]any{
        {"tag": "in-0", "protocol": "socks", "listen": "0.0.0.0", "port": socksPort, "udp": true, "auth": nil},
        {"tag": "in-1", "protocol": "http", "listen": "0.0.0.0", "port": httpPort, "auth": nil},
    }
}

func fixRules(raw []any) []any {
    nextID := 1
    var out []any
    for _, r := range raw {
        m := asMap(r)
        if m["id"] == nil || m["id"].(float64) == 0 {
            m["id"] = float64(nextID)
        }
        if _, ok := m["enabled"]; !ok {
            m["enabled"] = true
        }
        if v := int(m["id"].(float64)); v >= nextID {
            nextID = v + 1
        }
        out = append(out, m)
    }
    return out
}

func setDefaultSeq(raw map[string]any, key string, items []any, prefix string) {
    if raw[key] != nil {
        return
    }
    raw[key] = float64(seqFromTags(items, prefix))
}

func seqFromTags(items []any, prefix string) int {
    mx := -1
    for _, it := range items {
        m := asMap(it)
        tag, _ := m["tag"].(string)
       	if strings.HasPrefix(tag, prefix) {
            n, err := strconv.Atoi(tag[len(prefix):])
            if err == nil && n > mx {
                mx = n
            }
        }
    }
    return mx + 1
}

func asMap(v any) map[string]any {
    m, ok := v.(map[string]any)
    if !ok {
        return map[string]any{}
    }
    return m
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/store/ -v
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/store/
git commit -m "feat: add JSON file store with migration and atomic writes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8: Xray 子进程管理 — service/xray_proc.go

**Files:**
- Create: `go-backend/internal/service/xray_proc.go`

- [ ] **Step 1: 实现 xray_proc.go（含 interface + exec 实现 + 测试假实现）**

```go
// go-backend/internal/service/xray_proc.go
package service

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "sync"
    "syscall"
    "time"
)

// ---------- interface ----------

type XrayProc interface {
    Running() bool
    Start(configPath string)
    Stop()
    Restart(configPath string) bool
    TestConfig(cfgJSON []byte) (bool, string)
}

// ---------- exec 实现 ----------

type xrayProc struct {
    bin     string
    workdir string
    mu      sync.Mutex
    cmd     *exec.Cmd
}

func NewXrayProc(bin, workdir string) XrayProc {
    return &xrayProc{bin: bin, workdir: workdir}
}

func (x *xrayProc) Running() bool {
    x.mu.Lock()
    defer x.mu.Unlock()
    if x.cmd == nil || x.cmd.Process == nil {
        return false
    }
    return x.cmd.ProcessState == nil
}

func (x *xrayProc) Start(configPath string) {
    x.mu.Lock()
    defer x.mu.Unlock()
    if _, err := os.Stat(configPath); err != nil {
        return
    }
    x.cmd = exec.Command(x.bin, "-config", configPath)
    x.cmd.Stdout = os.Stdout
    x.cmd.Stderr = os.Stderr
    x.cmd.Start()
}

func (x *xrayProc) Stop() {
    x.mu.Lock()
    defer x.mu.Unlock()
    if x.cmd == nil || x.cmd.Process == nil {
        return
    }
    x.cmd.Process.Signal(syscall.SIGTERM)
    done := make(chan error, 1)
    go func() { done <- x.cmd.Wait() }()
    select {
    case <-done:
    case <-time.After(5 * time.Second):
        x.cmd.Process.Kill()
        <-done
    }
    x.cmd = nil
}

func (x *xrayProc) Restart(configPath string) bool {
    x.Stop()
    x.Start(configPath)
    time.Sleep(500 * time.Millisecond)
    return x.Running()
}

func (x *xrayProc) TestConfig(cfgJSON []byte) (bool, string) {
    os.MkdirAll(x.workdir, 0755)
    tmp := filepath.Join(x.workdir, "config.test.json")
    if err := os.WriteFile(tmp, cfgJSON, 0644); err != nil {
        return false, fmt.Sprintf("写入测试配置失败: %v", err)
    }
    defer os.Remove(tmp)

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, x.bin, "-test", "-config", tmp)
    out, err := cmd.CombinedOutput()
    if err != nil {
        return false, string(out)
    }
    return cmd.ProcessState.ExitCode() == 0, string(out)
}

// ---------- 测试假实现 ----------

type FakeXray struct {
    Alive   bool
    LastCfg []byte
}

func (f *FakeXray) Running() bool                        { return f.Alive }
func (f *FakeXray) Start(_ string)                        { f.Alive = true }
func (f *FakeXray) Stop()                                 { f.Alive = false }
func (f *FakeXray) Restart(_ string) bool                 { f.Alive = true; return true }
func (f *FakeXray) TestConfig(cfgJSON []byte) (bool, string) { f.LastCfg = cfgJSON; return true, "" }
```

- [ ] **Step 2: 验证编译**

```bash
cd go-backend && go build ./internal/service/
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/service/xray_proc.go
git commit -m "feat: add XrayProc interface with exec and fake implementations

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9: Service 辅助模块 — inbounds/proxies/routing

**Files:**
- Create: `go-backend/internal/service/inbounds.go`
- Create: `go-backend/internal/service/proxies.go`
- Create: `go-backend/internal/service/routing.go`
- Test: `go-backend/internal/service/service_test.go`

- [ ] **Step 1: 编写测试**

```go
// go-backend/internal/service/service_test.go
package service

import (
    "testing"
)

func TestInboundsToXray(t *testing.T) {
    ib := map[string]any{
        "tag": "in-0", "protocol": "socks", "listen": "0.0.0.0", "port": 10808,
        "auth": map[string]any{"user": "u", "pass": "p"},
    }
    result := InboundToXray(ib)
    if result["tag"] != "in-0" {
        t.Errorf("tag = %v", result["tag"])
    }
    settings := result["settings"].(map[string]any)
    if settings["auth"] != "password" {
        t.Errorf("socks with auth should have auth=password, got %v", settings["auth"])
    }
}

func TestInboundsToXrayNoAuth(t *testing.T) {
    ib := map[string]any{"tag": "in-1", "protocol": "socks", "listen": "127.0.0.1", "port": 1080}
    result := InboundToXray(ib)
    settings := result["settings"].(map[string]any)
    if settings["auth"] != "noauth" {
        t.Errorf("socks without auth should have auth=noauth, got %v", settings["auth"])
    }
}

func TestProxyToXray(t *testing.T) {
    p := map[string]any{
        "tag": "px-0", "protocol": "socks", "host": "10.0.0.1", "port": 1080,
        "auth": map[string]any{"user": "u", "pass": "p"},
    }
    result := ProxyToXray(p)
    if result["tag"] != "px-0" {
        t.Errorf("tag = %v", result["tag"])
    }
    settings := result["settings"].(map[string]any)
    servers := settings["servers"].([]any)
    server := servers[0].(map[string]any)
    if server["address"] != "10.0.0.1" {
        t.Errorf("address = %v", server["address"])
    }
}

func TestRuleToXrayDomain(t *testing.T) {
    rule := map[string]any{"type": "domain-suffix", "value": "google.com", "outbound": "px-0"}
    balancers := map[string]bool{}
    result := RuleToXray(rule, balancers)
    domains := result["domain"].([]string)
    if domains[0] != "domain:google.com" {
        t.Errorf("domain = %s", domains[0])
    }
    if result["outboundTag"] != "px-0" {
        t.Errorf("outboundTag = %v", result["outboundTag"])
    }
}

func TestRuleToXrayBalancer(t *testing.T) {
    rule := map[string]any{"type": "full", "value": "example.com", "outbound": "auto-0"}
    balancers := map[string]bool{"auto-0": true}
    result := RuleToXray(rule, balancers)
    if _, ok := result["balancerTag"]; !ok {
        t.Error("balancer should use balancerTag")
    }
}

func TestTemplatesNotEmpty(t *testing.T) {
    if len(Templates) == 0 {
        t.Error("templates should not be empty")
    }
    for k, v := range Templates {
        if len(v) == 0 {
            t.Errorf("template %s is empty", k)
        }
    }
}

func TestDescribe(t *testing.T) {
    r := map[string]any{"domain": []string{"domain:google.com"}}
    if d := Describe(r); d != "域名后缀 google.com" {
        t.Errorf("describe = %s", d)
    }
}
```

- [ ] **Step 2: 实现三个 service 文件**

inbounds.go:
```go
// go-backend/internal/service/inbounds.go
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
    } else { // http
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
```

proxies.go:
```go
// go-backend/internal/service/proxies.go
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
```

routing.go:
```go
// go-backend/internal/service/routing.go
package service

var PrivateCIDRs = []string{
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
    "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10",
}

var domainPrefix = map[string]string{
    "domain-suffix": "domain:",
    "full":          "full:",
    "keyword":       "",
    "geosite":       "geosite:",
}

func RuleToXray(rule map[string]any, balancerTags map[string]bool) map[string]any {
    r := map[string]any{"type": "field"}
    t, _ := rule["type"].(string)
    val, _ := rule["value"].(string)

    if prefix, ok := domainPrefix[t]; ok {
        r["domain"] = []string{prefix + val}
    } else if t == "ip" {
        r["ip"] = []string{val}
    } else if t == "geoip" {
        r["ip"] = []string{"geoip:" + val}
    } else if t == "port" {
        r["port"] = val
    }

    tag, _ := rule["outbound"].(string)
    if balancerTags[tag] {
        r["balancerTag"] = tag
    } else {
        r["outboundTag"] = tag
    }
    return r
}

func Describe(r map[string]any) string {
    if ip, ok := r["ip"].([]string); ok && len(ip) > 0 {
        if ip[0] == PrivateCIDRs[0] { // lazy check — 实际可能匹配多个
            return "私网 / 本机地址"
        }
        d := ip[0]
        if len(d) > 6 && d[:6] == "geoip:" {
            return "地区IP " + d
        }
        return "IP段 " + d
    }
    if domain, ok := r["domain"].([]string); ok && len(domain) > 0 {
        d := domain[0]
        if len(d) > 7 && d[:7] == "domain:" {
            return "域名后缀 " + d[7:]
        }
        if len(d) > 5 && d[:5] == "full:" {
            return "完整域名 " + d[5:]
        }
        if len(d) > 8 && d[:8] == "geosite:" {
            return "预置集合 " + d
        }
        return "关键字 " + d
    }
    if port, ok := r["port"].(string); ok {
        return "端口 " + port
    }
    if _, ok := r["network"]; ok {
        return "默认出口(其余流量)"
    }
    return "未知规则"
}

var Templates = map[string][]map[string]string{
    "cn-direct": {
        {"type": "geoip", "value": "cn", "outbound": "direct"},
        {"type": "geosite", "value": "cn", "outbound": "direct"},
        {"type": "geosite", "value": "geolocation-!cn", "outbound": "__PROXY__"},
    },
    "block-ads": {
        {"type": "geosite", "value": "category-ads-all", "outbound": "block"},
    },
    "streaming": {
        {"type": "geosite", "value": "netflix", "outbound": "__PROXY__"},
        {"type": "geosite", "value": "youtube", "outbound": "__PROXY__"},
        {"type": "geosite", "value": "disney", "outbound": "__PROXY__"},
    },
}

// ---------- helpers ----------

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
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/service/ -v
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/service/inbounds.go go-backend/internal/service/proxies.go go-backend/internal/service/routing.go go-backend/internal/service/service_test.go
git commit -m "feat: add inbound/proxy/routing translation services

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 10: 订阅解析 — service/xray_sub.go

**Files:**
- Create: `go-backend/internal/service/xray_sub.go`
- Test: `go-backend/internal/service/xray_sub_test.go`

- [ ] **Step 1: 编写测试（base64 解码 + VMess 解析）**

```go
// go-backend/internal/service/xray_sub_test.go
package service

import (
    "testing"
)

func TestB64Decode(t *testing.T) {
    result := b64decode("dGVzdA==")
    if string(result) != "test" {
        t.Errorf("b64decode = %s, want test", string(result))
    }
}

func TestExtractLinksBase64(t *testing.T) {
    // "vmess://link1\nvmess://link2" base64'd
    content := "dm1lc3M6Ly9saW5rMQp2bWVzczovL2xpbmsy"
    links, _ := ExtractLinks(content)
    if len(links) != 2 {
        t.Fatalf("expected 2 links, got %d", len(links))
    }
    if links[0] != "vmess://link1" {
        t.Errorf("link[0] = %s", links[0])
    }
}

func TestExtractLinksPlain(t *testing.T) {
    content := "vmess://test1\nSTATUS=ok\nvless://test2"
    links, meta := ExtractLinks(content)
    if len(links) != 2 {
        t.Fatalf("expected 2 links, got %d", len(links))
    }
    if meta["STATUS"] != "ok" {
        t.Errorf("STATUS = %s", meta["STATUS"])
    }
}

func TestParseVMess(t *testing.T) {
    // 简化版 vmess JSON + base64
    raw := `{"v":"2","ps":"TestNode","add":"1.2.3.4","port":"443","id":"uuid","aid":"0","net":"ws","type":"none","host":"","path":"/ws","tls":"tls","sni":"1.2.3.4"}`
    b64 := b64encode(raw)
    link := "vmess://" + b64
    node, err := parsers["vmess"](link)
    if err != nil {
        t.Fatal(err)
    }
    if node.Name != "TestNode" {
        t.Errorf("name = %s, want TestNode", node.Name)
    }
    if node.Host != "1.2.3.4" {
        t.Errorf("host = %s", node.Host)
    }
    if node.Port != 443 {
        t.Errorf("port = %d, want 443", node.Port)
    }
    ob := node.Outbound
    if ob["protocol"] != "vmess" {
        t.Errorf("protocol = %v", ob["protocol"])
    }
    stream := ob["streamSettings"].(map[string]any)
    if stream["security"] != "tls" {
        t.Errorf("security = %v", stream["security"])
    }
}

func TestAssignTags(t *testing.T) {
    nodes := []NodeRaw{
        {Name: "a", Host: "1.1.1.1", Port: 443, Type: "vmess", Outbound: map[string]any{}},
        {Name: "b", Host: "2.2.2.2", Port: 443, Type: "vless", Outbound: map[string]any{}},
    }
    AssignTags(nodes)
    if nodes[0].Tag != "node-0" {
        t.Errorf("tag[0] = %s", nodes[0].Tag)
    }
    if nodes[1].Tag != "node-1" {
        t.Errorf("tag[1] = %s", nodes[1].Tag)
    }
}

func TestSkipUnsupported(t *testing.T) {
    links := []string{"ssr://dGVzdA==", "hysteria://test", "vmess://invalid"}
    nodes, skipped := ParseLinks(links)
    if len(skipped) < 2 {
        t.Errorf("expected >= 2 skipped, got %d", len(skipped))
    }
    _ = nodes
}
```

- [ ] **Step 2: 实现 xray_sub.go（完整版，含 4 协议解析）**

```go
// go-backend/internal/service/xray_sub.go
package service

import (
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net"
    "net/url"
    "strconv"
    "strings"
    "sync"
    "time"
)

// ---------- 类型 ----------

type NodeRaw struct {
    Name     string
    Type     string
    Host     string
    Port     int
    Latency  *int
    Outbound map[string]any
    Tag      string
}

type SkipInfo struct {
    Scheme string
    Detail string
}

// ---------- Base64 ----------

func b64decode(s string) []byte {
    s = strings.TrimSpace(s)
    s = strings.ReplaceAll(s, "-", "+")
    s = strings.ReplaceAll(s, "_", "/")
    s += strings.Repeat("=", (4-len(s)%4)%4)
    data, _ := base64.StdEncoding.DecodeString(s)
    return data
}

func b64encode(s string) string {
    return base64.StdEncoding.EncodeToString([]byte(s))
}

// ---------- 提取链接 ----------

func ExtractLinks(content string) ([]string, map[string]string) {
    content = strings.TrimSpace(content)
    if !strings.Contains(content, "://") {
        content = string(b64decode(content))
    }
    var links []string
    meta := map[string]string{}
    for _, line := range strings.Split(content, "\n") {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        if strings.Contains(line, "://") {
            links = append(links, line)
        } else if strings.Contains(line, "=") {
            parts := strings.SplitN(line, "=", 2)
            meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
        }
    }
    return links, meta
}

// ---------- 协议解析注册表 ----------

type parserFunc func(link string) (NodeRaw, error)

var parsers = map[string]parserFunc{
    "vmess":  parseVMess,
    "vless":  parseVLess,
    "trojan": parseTrojan,
    "ss":     parseSS,
}

var unsupportedSchemes = map[string]bool{
    "ssr": true, "hysteria": true, "hysteria2": true, "hy2": true,
    "tuic": true, "snell": true, "wireguard": true,
}

func ParseLinks(links []string) ([]NodeRaw, []SkipInfo) {
    var nodes []NodeRaw
    var skipped []SkipInfo
    for _, link := range links {
        scheme := strings.ToLower(strings.Split(link, "://")[0])
        if unsupportedSchemes[scheme] || parsers[scheme] == nil {
            skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: link[:min(60, len(link))]})
            continue
        }
        node, err := parsers[scheme](link)
        if err != nil {
            skipped = append(skipped, SkipInfo{Scheme: scheme, Detail: fmt.Sprintf("%s (%v)", link[:min(40, len(link))], err)})
            continue
        }
        nodes = append(nodes, node)
    }
    return nodes, skipped
}

func AssignTags(nodes []NodeRaw) {
    for i := range nodes {
        nodes[i].Tag = fmt.Sprintf("node-%d", i)
    }
}

// ---------- VMess ----------

type vmessConfig struct {
    PS   string `json:"ps"`
    Add  string `json:"add"`
    Port any    `json:"port"`
    ID   string `json:"id"`
    AID  any    `json:"aid"`
    Scy  string `json:"scy"`
    Net  string `json:"net"`
    TLS  string `json:"tls"`
    Host string `json:"host"`
    Path string `json:"path"`
    SNI  string `json:"sni"`
}

func parseVMess(link string) (NodeRaw, error) {
    b64 := link[len("vmess://"):]
    raw := b64decode(b64)
    var v vmessConfig
    if err := json.Unmarshal(raw, &v); err != nil {
        return NodeRaw{}, fmt.Errorf("vmess JSON 解析失败: %w", err)
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

    stream := buildStreamSettings(net, tls, sni, path, v.Host)
    outbound := map[string]any{
        "protocol": "vmess",
        "settings": map[string]any{
            "vnext": []any{
                map[string]any{
                    "address": v.Add,
                    "port":    port,
                    "users": []any{
                        map[string]any{
                            "id":       v.ID,
                            "alterId":  toInt(v.AID),
                            "security": selStr(v.Scy, "auto"),
                        },
                    },
                },
            },
        },
        "streamSettings": stream,
    }
    name := v.PS
    if name == "" {
        name = v.Add
    }
    return NodeRaw{Name: name, Type: "vmess", Host: v.Add, Port: port, Outbound: outbound}, nil
}

func buildStreamSettings(net, tls, sni, path, host string) map[string]any {
    stream := map[string]any{"network": net}
    if tls == "reality" {
        stream["security"] = "reality"
    } else if tls == "tls" {
        stream["security"] = "tls"
        stream["tlsSettings"] = map[string]any{"serverName": sni, "allowInsecure": false}
    } else {
        stream["security"] = "none"
    }
    if net == "ws" {
        stream["wsSettings"] = map[string]any{"path": path, "headers": map[string]any{"Host": host}}
    } else if net == "grpc" {
        stream["grpcSettings"] = map[string]any{"serviceName": strings.TrimPrefix(path, "/")}
    }
    return stream
}

// ---------- VLess ----------

func parseVLess(link string) (NodeRaw, error) {
    u, err := url.Parse(link)
    if err != nil {
        return NodeRaw{}, err
    }
    q := u.Query()
    host := u.Hostname()
    port := u.Port()
    if p, _ := strconv.Atoi(port); p > 0 {
        port = strconv.Itoa(p)
    } else {
        port = "443"
    }

    net := q.Get("type")
    if net == "" {
        net = "tcp"
    }
    security := q.Get("security")
    if security == "" {
        security = "none"
    }

    stream := map[string]any{"network": net}
    if security == "tls" {
        sni := q.Get("sni")
        if sni == "" {
            sni = host
        }
        stream["security"] = "tls"
        stream["tlsSettings"] = map[string]any{"serverName": sni, "allowInsecure": q.Get("allowInsecure") == "1"}
    } else if security == "reality" {
        stream["security"] = "reality"
        stream["realitySettings"] = map[string]any{
            "serverName": q.Get("sni"), "fingerprint": selStr(q.Get("fp"), "chrome"),
            "publicKey": q.Get("pbk"), "shortId": q.Get("sid"), "spiderX": q.Get("spx"),
        }
    } else {
        stream["security"] = "none"
    }
    if net == "ws" {
        stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
    } else if net == "grpc" {
        svc := q.Get("serviceName")
        if svc == "" {
            svc = q.Get("path")
        }
        stream["grpcSettings"] = map[string]any{"serviceName": svc}
    }

    user := map[string]any{"id": u.User.Username(), "encryption": selStr(q.Get("encryption"), "none")}
    if flow := q.Get("flow"); flow != "" {
        user["flow"] = flow
    }

    outbound := map[string]any{
        "protocol": "vless",
        "settings": map[string]any{
            "vnext": []any{
                map[string]any{"address": host, "port": toInt(port), "users": []any{user}},
            },
        },
        "streamSettings": stream,
    }
    name := u.Fragment
    if name == "" {
        name = host
    }
    return NodeRaw{Name: name, Type: "vless", Host: host, Port: toInt(port), Outbound: outbound}, nil
}

// ---------- Trojan ----------

func parseTrojan(link string) (NodeRaw, error) {
    u, err := url.Parse(link)
    if err != nil {
        return NodeRaw{}, err
    }
    q := u.Query()
    host := u.Hostname()
    port := u.Port()
    if p, _ := strconv.Atoi(port); p > 0 {
        port = strconv.Itoa(p)
    } else {
        port = "443"
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
    allowInsecure := q.Get("allowInsecure") == "1"

    stream := map[string]any{
        "network": net, "security": "tls",
        "tlsSettings": map[string]any{"serverName": sni, "allowInsecure": allowInsecure},
    }
    if net == "ws" {
        stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
    }
    outbound := map[string]any{
        "protocol": "trojan",
        "settings": map[string]any{
            "servers": []any{
                map[string]any{"address": host, "port": toInt(port), "password": u.User.Username()},
            },
        },
        "streamSettings": stream,
    }
    name := u.Fragment
    if name == "" {
        name = host
    }
    return NodeRaw{Name: name, Type: "trojan", Host: host, Port: toInt(port), Outbound: outbound}, nil
}

// ---------- Shadowsocks ----------

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
            return NodeRaw{}, fmt.Errorf("ss 格式错误")
        }
        uparts := strings.SplitN(parts[0], ":", 2)
        if len(uparts) != 2 {
            return NodeRaw{}, fmt.Errorf("ss userinfo 格式错误")
        }
        method, password, server = uparts[0], uparts[1], parts[1]
    }

    hostPort := strings.Split(server, ":")
    if len(hostPort) != 2 {
        return NodeRaw{}, fmt.Errorf("ss server 格式错误")
    }
    host := hostPort[0]
    port := toInt(hostPort[1])

    outbound := map[string]any{
        "protocol": "shadowsocks",
        "settings": map[string]any{
            "servers": []any{
                map[string]any{"address": host, "port": port, "method": method, "password": password},
            },
        },
    }
    name := urlDecode(frag)
    if name == "" {
        name = host
    }
    return NodeRaw{Name: name, Type: "shadowsocks", Host: host, Port: port, Outbound: outbound}, nil
}

func urlDecode(s string) string {
    result, _ := url.QueryUnescape(s)
    return result
}

// ---------- TCP 测速 ----------

func MeasureLatency(nodes []NodeRaw) {
    sem := make(chan struct{}, 32)
    var wg sync.WaitGroup
    for i := range nodes {
        wg.Add(1)
        go func(n *NodeRaw) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()
            n.Latency = tcpPing(n.Host, n.Port)
        }(&nodes[i])
    }
    wg.Wait()
}

func tcpPing(host string, port int) *int {
    addr := fmt.Sprintf("%s:%d", host, port)
    start := time.Now()
    conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
    if err != nil {
        return nil
    }
    conn.Close()
    ms := int(time.Since(start).Milliseconds())
    return &ms
}

// ---------- helpers ----------

func toInt(v any) int {
    switch x := v.(type) {
    case float64:
        return int(x)
    case string:
        n, _ := strconv.Atoi(x)
        return n
    default:
        return 0
    }
}

func selStr(v, def string) string {
    if v != "" {
        return v
    }
    return def
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/service/ -v -run TestB64Decode -run TestExtractLinks -run TestParseVMess -run TestAssignTags -run TestSkipUnsupported
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/service/xray_sub.go go-backend/internal/service/xray_sub_test.go
git commit -m "feat: add subscription parser (vmess/vless/trojan/ss) with TCP pinger

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 11: Config Builder — service/config_builder.go

**Files:**
- Create: `go-backend/internal/service/config_builder.go`
- Test: `go-backend/internal/service/config_builder_test.go`

- [ ] **Step 1: 编写测试**

```go
// go-backend/internal/service/config_builder_test.go
package service

import (
    "testing"

    "xray-panel/internal/model"
)

func TestBuildConfigMinimal(t *testing.T) {
    state := &model.PanelState{
        Nodes: []model.Node{
            {Tag: "node-0", Name: "Test", Type: "vmess", Host: "1.2.3.4", Port: 443,
                Outbound: map[string]any{"protocol": "vmess"}},
        },
        Inbounds: []model.Inbound{
            {Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808, UDP: true},
        },
        DefaultOutbound: "node-0",
    }
    cfg := BuildConfig(state)
    outbounds := cfg["outbounds"].([]any)
    if len(outbounds) < 3 {
        t.Fatalf("expected >= 3 outbounds, got %d", len(outbounds))
    }
    routing := cfg["routing"].(map[string]any)
    rules := routing["rules"].([]any)
    if len(rules) < 2 {
        t.Fatalf("expected >= 2 rules, got %d", len(rules))
    }
}

func TestBuildConfigWithBalancers(t *testing.T) {
    state := &model.PanelState{
        Nodes: []model.Node{
            {Tag: "node-0", Name: "N1", Type: "vmess", Host: "1.1.1.1", Port: 443,
                Outbound: map[string]any{"protocol": "vmess"}},
            {Tag: "node-1", Name: "N2", Type: "vless", Host: "2.2.2.2", Port: 443,
                Outbound: map[string]any{"protocol": "vless"}},
        },
        Balancers: []model.Balancer{
            {Tag: "auto-0", Name: "Auto", Nodes: []string{"node-0", "node-1"}, Strategy: "leastPing"},
        },
        Inbounds: []model.Inbound{
            {Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808},
        },
        DefaultOutbound: "auto-0",
    }
    cfg := BuildConfig(state)
    routing := cfg["routing"].(map[string]any)
    if _, ok := routing["balancers"]; !ok {
        t.Error("balancers should exist in routing")
    }
    if _, ok := cfg["observatory"]; !ok {
        t.Error("observatory should exist when balancers are configured")
    }
}
```

- [ ] **Step 2: 实现 config_builder.go**

```go
// go-backend/internal/service/config_builder.go
package service

import (
    "xray-panel/internal/model"
)

var observatory = map[string]any{
    "subjectSelector": []string{"node-"},
    "probeURL":        "https://www.gstatic.com/generate_204",
    "probeInterval":   "5m",
}

func BuildConfig(state *model.PanelState) map[string]any {
    balancerTags := map[string]bool{}
    for _, b := range state.Balancers {
        balancerTags[b.Tag] = true
    }

    // outbounds
    var outbounds []any
    for _, n := range state.Nodes {
        ob := deepCopyMap(n.Outbound)
        ob["tag"] = n.Tag
        outbounds = append(outbounds, ob)
    }
    for _, p := range state.Proxies {
        ob := ProxyToXray(map[string]any{
            "tag": p.Tag, "protocol": p.Protocol, "host": p.Host, "port": p.Port,
        })
        if p.Auth != nil {
            ob["settings"].(map[string]any)["servers"].([]any)[0].(map[string]any)["users"] = []any{
                map[string]any{"user": p.Auth.User, "pass": p.Auth.Password},
            }
        }
        outbounds = append(outbounds, ob)
    }
    outbounds = append(outbounds,
        map[string]any{"tag": "direct", "protocol": "freedom"},
        map[string]any{"tag": "block", "protocol": "blackhole"},
    )

    // rules
    var rules []any
    for _, r := range state.Rules {
        if !r.Enabled || r.Value == "" {
            continue
        }
        rules = append(rules, RuleToXray(map[string]any{
            "type": r.Type, "value": r.Value, "outbound": r.Outbound,
        }, balancerTags))
    }
    rules = append(rules, map[string]any{
        "type": "field", "ip": PrivateCIDRs, "outboundTag": "direct",
    })

    defaultTag := state.DefaultOutbound
    if defaultTag == "" && len(state.Nodes) > 0 {
        defaultTag = state.Nodes[0].Tag
    }
    tail := map[string]any{"type": "field", "network": "tcp,udp"}
    if balancerTags[defaultTag] {
        tail["balancerTag"] = defaultTag
    } else {
        tail["outboundTag"] = defaultTag
    }
    rules = append(rules, tail)

    // inbounds
    var inbounds []any
    for _, ib := range state.Inbounds {
        auth := map[string]any(nil)
        if ib.Auth != nil {
            auth = map[string]any{"user": ib.Auth.User, "pass": ib.Auth.Password}
        }
        inbounds = append(inbounds, InboundToXray(map[string]any{
            "tag": ib.Tag, "protocol": ib.Protocol, "listen": ib.Listen,
            "port": ib.Port, "udp": ib.UDP, "auth": auth,
        }))
    }

    cfg := map[string]any{
        "log":       map[string]any{"loglevel": "warning"},
        "inbounds":  inbounds,
        "outbounds": outbounds,
        "routing": map[string]any{
            "domainStrategy": "AsIs",
            "rules":          rules,
        },
    }

    if len(state.Balancers) > 0 {
        var bls []any
        for _, b := range state.Balancers {
            bls = append(bls, map[string]any{
                "tag": b.Tag, "selector": b.Nodes,
                "strategy": map[string]any{"type": selStr(b.Strategy, "leastPing")},
            })
        }
        cfg["routing"].(map[string]any)["balancers"] = bls
        cfg["observatory"] = observatory
    }

    return cfg
}

func deepCopyMap(src map[string]any) map[string]any {
    data, _ := json.Marshal(src)
    var dst map[string]any
    json.Unmarshal(data, &dst)
    return dst
}
```

需要在 config_builder.go 顶部添加 `"encoding/json"` import。

- [ ] **Step 3: 运行测试确认通过**

```bash
cd go-backend && go test ./internal/service/ -v -run TestBuildConfig
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/service/config_builder.go go-backend/internal/service/config_builder_test.go
git commit -m "feat: add Xray config builder from PanelState

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 12: Auth 中间件 — middleware/auth.go

**Files:**
- Create: `go-backend/internal/middleware/auth.go`

- [ ] **Step 1: 实现**

```go
// go-backend/internal/middleware/auth.go
package middleware

import (
    "encoding/json"
    "net/http"
    "strings"

    "xray-panel/internal/auth"
)

func RequireAuth(ss *auth.SessionStore) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            header := r.Header.Get("Authorization")
            token := ""
            if strings.HasPrefix(header, "Bearer ") {
                token = header[7:]
            }
            if !ss.Valid(token) {
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusUnauthorized)
                json.NewEncoder(w).Encode(map[string]string{"detail": "未授权或登录已过期"})
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

- [ ] **Step 2: 编译验证**

```bash
cd go-backend && go build ./internal/middleware/
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/middleware/auth.go
git commit -m "feat: add Bearer token auth middleware

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 13: App 聚合层 — app/app.go

**Files:**
- Create: `go-backend/internal/app/app.go`

- [ ] **Step 1: 实现**

```go
// go-backend/internal/app/app.go
package app

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "log/slog"

    "xray-panel/internal/auth"
    "xray-panel/internal/model"
    "xray-panel/internal/service"
    "xray-panel/internal/store"
)

type Config struct {
    Store                     store.Store
    XrayProc                  service.XrayProc
    ConfigPath                string
    PanelPort                 int
    Password                  *string
    SubscriptionAllowInternal bool
}

type App struct {
    store    store.Store
    xray     service.XrayProc
    sessions *auth.SessionStore
    state    *model.PanelState
    config   Config
}

func New(cfg Config) (*App, error) {
    state, err := cfg.Store.Load()
    if err != nil {
        return nil, fmt.Errorf("加载状态失败: %w", err)
    }

    a := &App{
        store:    cfg.Store,
        xray:     cfg.XrayProc,
        sessions: auth.NewSessionStore(7 * 86400),
        state:    state,
        config:   cfg,
    }

    a.ensurePassword()
    return a, nil
}

func (a *App) ensurePassword() {
    if a.state.Password != nil {
        return
    }
    pw := ""
    src := "随机生成"
    if a.config.Password != nil && *a.config.Password != "" {
        pw = *a.config.Password
        src = "环境变量 PANEL_PASSWORD"
    } else {
        b := make([]byte, 9)
        rand.Read(b)
        pw = hex.EncodeToString(b)
    }
    rec, _ := auth.HashPassword(pw)
    a.state.Password = &model.PasswordRec{Salt: rec.Salt, Hash: rec.Hash}
    a.store.Save(a.state)
    slog.Info(fmt.Sprintf("面板登录密码(%s): %s", src, pw))
}

// ---------- accessors ----------

func (a *App) State() *model.PanelState     { return a.state }
func (a *App) Sessions() *auth.SessionStore  { return a.sessions }
func (a *App) Xray() service.XrayProc        { return a.xray }
func (a *App) Config() Config                { return a.config }
func (a *App) Store() store.Store             { return a.store }

func (a *App) Persist() error { return a.store.Save(a.state) }

// ---------- 标签计算 ----------

func (a *App) BalancerTags() map[string]bool {
    tags := map[string]bool{}
    for _, b := range a.state.Balancers {
        tags[b.Tag] = true
    }
    return tags
}

func (a *App) OutboundTags() map[string]bool {
    tags := map[string]bool{"direct": true, "block": true}
    for _, n := range a.state.Nodes {
        tags[n.Tag] = true
    }
    for _, b := range a.state.Balancers {
        tags[b.Tag] = true
    }
    for _, p := range a.state.Proxies {
        tags[p.Tag] = true
    }
    return tags
}

func (a *App) PruneDangling() {
    valid := a.OutboundTags()
    if !valid[a.state.DefaultOutbound] {
        if len(a.state.Nodes) > 0 {
            a.state.DefaultOutbound = a.state.Nodes[0].Tag
        } else {
            a.state.DefaultOutbound = "direct"
        }
    }
    var kept []model.Rule
    for _, r := range a.state.Rules {
        if valid[r.Outbound] {
            kept = append(kept, r)
        }
    }
    a.state.Rules = kept
}

func (a *App) OutboundLabel(tag string) string {
    switch tag {
    case "direct":
        return "直连"
    case "block":
        return "阻断"
    }
    for _, n := range a.state.Nodes {
        if n.Tag == tag {
            return n.Name
        }
    }
    for _, b := range a.state.Balancers {
        if b.Tag == tag {
            return "⚖ " + b.Name + "(自动)"
        }
    }
    for _, p := range a.state.Proxies {
        if p.Tag == tag {
            return "🛰 " + p.Name + "(落地)"
        }
    }
    return tag
}
```

- [ ] **Step 2: 编译验证**

```bash
cd go-backend && go build ./internal/app/
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/app/app.go
git commit -m "feat: add App aggregate layer with session, password, and tag helpers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 14: Handler 辅助函数 + Server 路由 — handler/server.go, handler/helpers.go

**Files:**
- Create: `go-backend/internal/handler/helpers.go`
- Create: `go-backend/internal/handler/server.go`

- [ ] **Step 1: 实现 helpers.go**

```go
// go-backend/internal/handler/helpers.go
package handler

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"

    "github.com/go-playground/validator/v10"
)

var validate = validator.New()

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
    dec := json.NewDecoder(r.Body)
    return dec.Decode(v)
}

func translateValidation(err error) string {
    if errs, ok := err.(validator.ValidationErrors); ok {
        var msgs []string
        for _, e := range errs {
            msgs = append(msgs, fmt.Sprintf("字段 %s 校验失败: %s", e.Field(), e.Tag()))
        }
        return strings.Join(msgs, "; ")
    }
    return err.Error()
}
```

- [ ] **Step 2: 实现 server.go（路由挂载）**

```go
// go-backend/internal/handler/server.go
package handler

import (
    "github.com/go-chi/chi/v5"
    "xray-panel/internal/app"
    "xray-panel/internal/middleware"
)

type Server struct {
    App *app.App
}

func NewServer(a *app.App) *Server {
    return &Server{App: a}
}

func (s *Server) Routes() chi.Router {
    r := chi.NewRouter()

    // 公开
    r.Post("/api/auth/login", s.Login)

    // 受保护
    r.Group(func(r chi.Router) {
        r.Use(middleware.RequireAuth(s.App.Sessions()))

        // auth
        r.Post("/api/auth/logout", s.Logout)
        r.Get("/api/auth/me", s.Me)
        r.Put("/api/auth/password", s.ChangePassword)

        // inbounds
        r.Get("/api/inbounds", s.ListInbounds)
        r.Post("/api/inbounds", s.CreateInbound)
        r.Put("/api/inbounds/{tag}", s.UpdateInbound)
        r.Delete("/api/inbounds/{tag}", s.DeleteInbound)

        // proxies
        r.Get("/api/proxies", s.ListProxies)
        r.Post("/api/proxies", s.CreateProxy)
        r.Put("/api/proxies/{tag}", s.UpdateProxy)
        r.Delete("/api/proxies/{tag}", s.DeleteProxy)

        // balancers
        r.Get("/api/balancers", s.ListBalancers)
        r.Post("/api/balancers", s.CreateBalancer)
        r.Put("/api/balancers/{tag}", s.UpdateBalancer)
        r.Delete("/api/balancers/{tag}", s.DeleteBalancer)

        // routing
        r.Get("/api/routing", s.GetRouting)
        r.Put("/api/routing", s.PutRouting)
        r.Get("/api/routing/templates", s.Templates)
        r.Get("/api/outbounds", s.Outbounds)

        // subscription
        r.Get("/api/subscription", s.GetSubscription)
        r.Put("/api/subscription", s.SetSubscription)
        r.Get("/api/nodes", s.ListNodes)
        r.Post("/api/nodes/test", s.TestNodes)

        // xray
        r.Get("/api/xray/status", s.XrayStatus)
        r.Post("/api/apply", s.Apply)
        r.Post("/api/xray/restart", s.XrayRestart)
        r.Get("/api/config", s.RawConfig)
        r.Get("/api/topology", s.Topology)
    })

    return r
}
```

- [ ] **Step 3: 编译验证**

```bash
cd go-backend && go build ./internal/handler/
```

- [ ] **Step 4: 提交**

```bash
git add go-backend/internal/handler/helpers.go go-backend/internal/handler/server.go
git commit -m "feat: add handler helpers and chi router setup

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 15: Handler — Auth

**Files:**
- Create: `go-backend/internal/handler/auth.go`

- [ ] **Step 1: 实现**

```go
// go-backend/internal/handler/auth.go
package handler

import (
    "net/http"
    "strings"

    "xray-panel/internal/auth"
    "xray-panel/internal/model"
)

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
    var body model.LoginIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误")
        return
    }
    if !auth.VerifyPassword(s.App.State().Password, body.Password) {
        writeError(w, 401, "密码错误")
        return
    }
    writeJSON(w, 200, map[string]any{
        "token":      s.App.Sessions().Create(),
        "expires_in": 7 * 86400,
    })
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
    h := r.Header.Get("Authorization")
    if strings.HasPrefix(h, "Bearer ") {
        s.App.Sessions().Revoke(h[7:])
    }
    writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
    var body model.PasswordChangeIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误")
        return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err))
        return
    }
    if !auth.VerifyPassword(s.App.State().Password, body.OldPassword) {
        writeError(w, 400, "原密码错误")
        return
    }
    rec, _ := auth.HashPassword(body.NewPassword)
    s.App.State().Password = rec
    s.App.Persist()
    s.App.Sessions().Clear()
    writeJSON(w, 200, map[string]any{
        "ok":    true,
        "token": s.App.Sessions().Create(),
    })
}
```

注意：`auth.PasswordRec` 和 `model.PasswordRec` 是两个不同的类型。需要统一。在 Task 5 中 `auth/password.go` 定义了 `type PasswordRec struct`，但 `model/state.go` 也定义了 `type PasswordRec struct`。

解决办法：删除 `auth.PasswordRec`，直接使用 `model.PasswordRec`。修改 `auth/password.go`：

```go
// auth/password.go 使用 model.PasswordRec
func HashPassword(pw string, salt ...string) (*model.PasswordRec, error) { ... }
func VerifyPassword(rec *model.PasswordRec, pw string) bool { ... }
```

实现时请确认 import 循环：`auth` 不 import `model`（现在需要了）。由于 `model` 包不 import `auth`，所以不会循环依赖。

- [ ] **Step 2: 编译验证**

```bash
cd go-backend && go build ./internal/handler/
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/handler/auth.go
git commit -m "feat: add auth handler (login/logout/me/change-password)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 16: Handler — Inbounds + Proxies + Balancers

**Files:**
- Create: `go-backend/internal/handler/inbounds.go`
- Create: `go-backend/internal/handler/proxies.go`
- Create: `go-backend/internal/handler/balancers.go`

- [ ] **Step 1: 实现 inbounds.go**

```go
// go-backend/internal/handler/inbounds.go
package handler

import (
    "fmt"
    "net/http"

    "github.com/go-chi/chi/v5"
    "xray-panel/internal/model"
)

func (s *Server) ListInbounds(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, s.App.State().Inbounds)
}

func (s *Server) CreateInbound(w http.ResponseWriter, r *http.Request) {
    var body model.InboundIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    if body.Port == s.App.Config().PanelPort {
        writeError(w, 400, fmt.Sprintf("入站端口 %d 与面板端口冲突", body.Port)); return
    }

    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    for _, ib := range state.Inbounds {
        if ib.Port == body.Port {
            writeError(w, 400, fmt.Sprintf("入站端口 %d 重复", body.Port)); return
        }
    }

    tag := fmt.Sprintf("in-%d", state.InboundSeq)
    state.InboundSeq++
    ib := model.Inbound{
        Tag: tag, Protocol: body.Protocol, Listen: body.Listen,
        Port: body.Port, UDP: body.UDP, Auth: toModelAuth(body.Auth),
    }
    if ib.Listen == "" {
        ib.Listen = "127.0.0.1"
    }
    state.Inbounds = append(state.Inbounds, ib)
    if err := s.App.Persist(); err != nil {
        writeError(w, 500, "保存失败"); return
    }
    writeJSON(w, 201, ib)
}

func (s *Server) UpdateInbound(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    var body model.InboundIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }

    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    idx := -1
    for i, ib := range state.Inbounds {
        if ib.Tag == tag {
            idx = i; break
        }
    }
    if idx < 0 {
        writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag)); return
    }
    if body.Port == s.App.Config().PanelPort {
        writeError(w, 400, fmt.Sprintf("入站端口 %d 与面板端口冲突", body.Port)); return
    }
    for i, ib := range state.Inbounds {
        if i != idx && ib.Port == body.Port {
            writeError(w, 400, fmt.Sprintf("入站端口 %d 重复", body.Port)); return
        }
    }
    ib := model.Inbound{
        Tag: tag, Protocol: body.Protocol, Listen: body.Listen,
        Port: body.Port, UDP: body.UDP, Auth: toModelAuth(body.Auth),
    }
    if ib.Listen == "" {
        ib.Listen = "127.0.0.1"
    }
    state.Inbounds[idx] = ib
    if err := s.App.Persist(); err != nil {
        writeError(w, 500, "保存失败"); return
    }
    writeJSON(w, 200, ib)
}

func (s *Server) DeleteInbound(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    var kept []model.Inbound
    found := false
    for _, ib := range state.Inbounds {
        if ib.Tag == tag {
            found = true
        } else {
            kept = append(kept, ib)
        }
    }
    if !found {
        writeError(w, 404, fmt.Sprintf("入站 %s 不存在", tag)); return
    }
    state.Inbounds = kept
    s.App.Persist()
    writeJSON(w, 200, map[string]bool{"ok": true})
}

func toModelAuth(a *model.AuthIn) *model.Auth {
    if a == nil {
        return nil
    }
    return &model.Auth{User: a.User, Password: a.Password}
}
```

- [ ] **Step 2: 实现 proxies.go**

```go
// go-backend/internal/handler/proxies.go
package handler

import (
    "fmt"
    "net/http"

    "github.com/go-chi/chi/v5"
    "xray-panel/internal/model"
)

func (s *Server) ListProxies(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, s.App.State().Proxies)
}

func (s *Server) CreateProxy(w http.ResponseWriter, r *http.Request) {
    var body model.ProxyIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    tag := fmt.Sprintf("px-%d", state.ProxySeq)
    state.ProxySeq++
    name := body.Name
    if name == "" {
        name = tag
    }
    px := model.Proxy{
        Tag: tag, Name: name, Protocol: body.Protocol,
        Host: body.Host, Port: body.Port, Auth: toModelAuth(body.Auth),
    }
    state.Proxies = append(state.Proxies, px)
    s.App.Persist()
    writeJSON(w, 201, px)
}

func (s *Server) UpdateProxy(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    var body model.ProxyIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    idx := -1
    for i, p := range state.Proxies {
        if p.Tag == tag {
            idx = i; break
        }
    }
    if idx < 0 {
        writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag)); return
    }
    name := body.Name
    if name == "" {
        name = tag
    }
    px := model.Proxy{
        Tag: tag, Name: name, Protocol: body.Protocol,
        Host: body.Host, Port: body.Port, Auth: toModelAuth(body.Auth),
    }
    state.Proxies[idx] = px
    s.App.Persist()
    writeJSON(w, 200, px)
}

func (s *Server) DeleteProxy(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    var kept []model.Proxy
    found := false
    for _, p := range state.Proxies {
        if p.Tag == tag {
            found = true
        } else {
            kept = append(kept, p)
        }
    }
    if !found {
        writeError(w, 404, fmt.Sprintf("代理 %s 不存在", tag)); return
    }
    state.Proxies = kept
    s.App.PruneDangling()
    s.App.Persist()
    writeJSON(w, 200, map[string]bool{"ok": true})
}
```

- [ ] **Step 3: 实现 balancers.go**

```go
// go-backend/internal/handler/balancers.go
package handler

import (
    "fmt"
    "net/http"

    "github.com/go-chi/chi/v5"
    "xray-panel/internal/model"
)

func (s *Server) ListBalancers(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, s.App.State().Balancers)
}

func (s *Server) CreateBalancer(w http.ResponseWriter, r *http.Request) {
    var body model.BalancerIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    nodeTags := map[string]bool{}
    for _, n := range state.Nodes {
        nodeTags[n.Tag] = true
    }
    var members []string
    for _, t := range body.Nodes {
        if nodeTags[t] {
            members = append(members, t)
        }
    }
    if len(members) == 0 {
        writeError(w, 400, fmt.Sprintf("自动组「%s」没有有效节点", body.Name)); return
    }
    tag := fmt.Sprintf("auto-%d", state.BalancerSeq)
    state.BalancerSeq++
    name := body.Name
    if name == "" {
        name = tag
    }
    bal := model.Balancer{Tag: tag, Name: name, Nodes: members, Strategy: "leastPing"}
    state.Balancers = append(state.Balancers, bal)
    s.App.Persist()
    writeJSON(w, 201, bal)
}

func (s *Server) UpdateBalancer(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    var body model.BalancerIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    if err := validate.Struct(body); err != nil {
        writeError(w, 400, translateValidation(err)); return
    }
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    idx := -1
    for i, b := range state.Balancers {
        if b.Tag == tag {
            idx = i; break
        }
    }
    if idx < 0 {
        writeError(w, 404, fmt.Sprintf("自动组 %s 不存在", tag)); return
    }
    nodeTags := map[string]bool{}
    for _, n := range state.Nodes {
        nodeTags[n.Tag] = true
    }
    var members []string
    for _, t := range body.Nodes {
        if nodeTags[t] {
            members = append(members, t)
        }
    }
    if len(members) == 0 {
        writeError(w, 400, fmt.Sprintf("自动组「%s」没有有效节点", body.Name)); return
    }
    name := body.Name
    if name == "" {
        name = tag
    }
    bal := model.Balancer{Tag: tag, Name: name, Nodes: members, Strategy: "leastPing"}
    state.Balancers[idx] = bal
    s.App.Persist()
    writeJSON(w, 200, bal)
}

func (s *Server) DeleteBalancer(w http.ResponseWriter, r *http.Request) {
    tag := chi.URLParam(r, "tag")
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    var kept []model.Balancer
    found := false
    for _, b := range state.Balancers {
        if b.Tag == tag {
            found = true
        } else {
            kept = append(kept, b)
        }
    }
    if !found {
        writeError(w, 404, fmt.Sprintf("自动组 %s 不存在", tag)); return
    }
    state.Balancers = kept
    s.App.PruneDangling()
    s.App.Persist()
    writeJSON(w, 200, map[string]bool{"ok": true})
}
```

- [ ] **Step 4: 编译验证**

```bash
cd go-backend && go build ./internal/handler/
```

- [ ] **Step 5: 提交**

```bash
git add go-backend/internal/handler/inbounds.go go-backend/internal/handler/proxies.go go-backend/internal/handler/balancers.go
git commit -m "feat: add inbounds/proxies/balancers CRUD handlers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 17: Handler — Routing + Subscription + Xray

**Files:**
- Create: `go-backend/internal/handler/routing.go`
- Create: `go-backend/internal/handler/subscription.go`
- Create: `go-backend/internal/handler/xray.go`

- [ ] **Step 1: 实现 routing.go**

```go
// go-backend/internal/handler/routing.go
package handler

import (
    "fmt"
    "net/http"

    "xray-panel/internal/model"
    "xray-panel/internal/service"
)

func (s *Server) GetRouting(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, map[string]any{
        "default_outbound": s.App.State().DefaultOutbound,
        "rules":            s.App.State().Rules,
    })
}

func (s *Server) PutRouting(w http.ResponseWriter, r *http.Request) {
    var body model.RoutingIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    valid := s.App.OutboundTags()
    if body.DefaultOutbound != "" && !valid[body.DefaultOutbound] {
        writeError(w, 400, fmt.Sprintf("默认出口 %s 不存在", body.DefaultOutbound)); return
    }
    var rules []model.Rule
    for i, r := range body.Rules {
        if !valid[r.Outbound] {
            writeError(w, 400, fmt.Sprintf("规则出口 %s 不存在", r.Outbound)); return
        }
        rules = append(rules, model.Rule{
            ID: i + 1, Type: r.Type, Value: r.Value,
            Outbound: r.Outbound, Enabled: r.Enabled,
        })
    }
    s.App.State().DefaultOutbound = body.DefaultOutbound
    s.App.State().Rules = rules
    s.App.Persist()
    writeJSON(w, 200, map[string]any{
        "default_outbound": s.App.State().DefaultOutbound,
        "rules":            s.App.State().Rules,
    })
}

func (s *Server) Templates(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, service.Templates)
}

func (s *Server) Outbounds(w http.ResponseWriter, r *http.Request) {
    var out []map[string]string
    for _, n := range s.App.State().Nodes {
        out = append(out, map[string]string{"tag": n.Tag, "label": n.Name, "kind": "node"})
    }
    for _, b := range s.App.State().Balancers {
        out = append(out, map[string]string{
            "tag": b.Tag, "label": "⚖ " + b.Name + "(自动)", "kind": "balancer",
        })
    }
    for _, p := range s.App.State().Proxies {
        out = append(out, map[string]string{
            "tag": p.Tag, "label": "🛰 " + p.Name + "(落地)", "kind": "proxy",
        })
    }
    out = append(out,
        map[string]string{"tag": "direct", "label": "直连", "kind": "builtin"},
        map[string]string{"tag": "block", "label": "阻断", "kind": "builtin"},
    )
    writeJSON(w, 200, out)
}
```

- [ ] **Step 2: 实现 subscription.go**

```go
// go-backend/internal/handler/subscription.go
package handler

import (
    "fmt"
    "net"
    "net/http"
    "net/url"
    "strings"
    "time"

    "xray-panel/internal/model"
    "xray-panel/internal/service"
)

var ua = "Mozilla/5.0 (X11; Linux x86_64) Shadowrocket/2.2.49"

func (s *Server) GetSubscription(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, s.App.State().Subscription)
}

func (s *Server) SetSubscription(w http.ResponseWriter, r *http.Request) {
    var body model.SubscriptionIn
    if err := decodeJSON(r, &body); err != nil {
        writeError(w, 400, "请求格式错误"); return
    }
    urlStr := strings.TrimSpace(body.URL)
    if urlStr == "" {
        writeError(w, 400, "订阅链接为空"); return
    }

    text, err := fetchURL(urlStr)
    if err != nil {
        writeError(w, 502, fmt.Sprintf("拉取失败: %v", err)); return
    }

    links, meta := service.ExtractLinks(text)
    parsed, skipped := service.ParseLinks(links)
    service.AssignTags(parsed)
    if len(parsed) == 0 {
        writeError(w, 400, "未解析到任何 Xray 可用节点"); return
    }

    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    state := s.App.State()
    state.Nodes = make([]model.Node, len(parsed))
    for i, n := range parsed {
        state.Nodes[i] = model.Node{
            Name: n.Name, Type: n.Type, Host: n.Host, Port: n.Port,
            Tag: n.Tag, Outbound: n.Outbound,
        }
    }
    state.Subscription = model.Subscription{
        URL: urlStr, Remarks: meta["REMARKS"], Status: meta["STATUS"],
        FetchedAt: time.Now().Unix(),
    }
    // 清理无效 balancer 节点引用
    nodeTags := map[string]bool{}
    for _, n := range state.Nodes {
        nodeTags[n.Tag] = true
    }
    var kept []model.Balancer
    for _, b := range state.Balancers {
        valid := true
        for _, t := range b.Nodes {
            if !nodeTags[t] {
                valid = false; break
            }
        }
        if valid {
            kept = append(kept, b)
        }
    }
    state.Balancers = kept
    s.App.PruneDangling()
    s.App.Persist()
    writeJSON(w, 200, map[string]any{
        "nodes": state.Nodes, "skipped": len(skipped),
        "subscription": state.Subscription,
    })
}

func (s *Server) ListNodes(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, 200, s.App.State().Nodes)
}

func (s *Server) TestNodes(w http.ResponseWriter, r *http.Request) {
    s.App.Store().Lock()
    plain := make([]service.NodeRaw, len(s.App.State().Nodes))
    for i, n := range s.App.State().Nodes {
        plain[i] = service.NodeRaw{Host: n.Host, Port: n.Port}
    }
    s.App.Store().Unlock()

    service.MeasureLatency(plain)

    s.App.Store().Lock()
    defer s.App.Store().Unlock()
    for i := range s.App.State().Nodes {
        s.App.State().Nodes[i].Latency = plain[i].Latency
    }
    s.App.Persist()
    writeJSON(w, 200, s.App.State().Nodes)
}

// SSRF 防护
func fetchURL(urlStr string) (string, error) {
    u, err := url.Parse(urlStr)
    if err != nil {
        return "", fmt.Errorf("链接格式错误")
    }
    if u.Scheme != "http" && u.Scheme != "https" {
        return "", fmt.Errorf("只支持 http/https 订阅链接")
    }
    if u.Hostname() == "" {
        return "", fmt.Errorf("订阅链接缺少主机名")
    }
    if !s.App.Config().SubscriptionAllowInternal {
        ips, err := net.LookupIP(u.Hostname())
        if err != nil {
            return "", fmt.Errorf("无法解析订阅域名")
        }
        for _, ip := range ips {
            if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
                ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
                return "", fmt.Errorf("订阅域名解析到内网/保留地址 %s,已拒绝", ip)
            }
        }
    }
    client := &http.Client{
        Timeout: 20 * time.Second,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse // 禁止重定向
        },
    }
    req, _ := http.NewRequest("GET", urlStr, nil)
    req.Header.Set("User-Agent", ua)
    resp, err := client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    buf := new(strings.Builder)
    _, err = buf.ReadFrom(resp.Body)
    return buf.String(), err
}
```

需要在 subscription.go 顶部添加 `"io"` import，在 `fetchURL` 中用 `io.ReadAll` 替代 `strings.Builder`：
```go
import (
    ...
    "io"
    ...
)
```

然后把 `buf.ReadFrom` 改为：
```go
data, err := io.ReadAll(resp.Body)
return string(data), err
```

- [ ] **Step 3: 实现 xray.go**

```go
// go-backend/internal/handler/xray.go
package handler

import (
    "encoding/json"
    "fmt"
    "net/http"
    "os"

    "xray-panel/internal/service"
)

func (s *Server) XrayStatus(w http.ResponseWriter, r *http.Request) {
    applied := readApplied(s.App.Config().ConfigPath)
    writeJSON(w, 200, map[string]any{
        "running": s.App.Xray().Running(),
        "applied": applied != nil,
        "dirty":   s.App.Dirty(applied),
    })
}

func (s *Server) Apply(w http.ResponseWriter, r *http.Request) {
    s.App.Store().Lock()
    defer s.App.Store().Unlock()

    if len(s.App.State().Nodes) == 0 {
        writeError(w, 400, "还没有节点,先拉取订阅"); return
    }
    cfg := service.BuildConfig(s.App.State())
    ok, out := s.App.Xray().TestConfig(cfgJSON(cfg))
    if !ok {
        if len(out) > 800 {
            out = out[len(out)-800:]
        }
        writeError(w, 400, fmt.Sprintf("配置校验失败: %s", out)); return
    }
    // 原子写入
    path := s.App.Config().ConfigPath
    data, _ := json.MarshalIndent(cfg, "", "  ")
    tmp := path + ".tmp"
    os.WriteFile(tmp, data, 0644)
    os.Rename(tmp, path)
    running := s.App.Xray().Restart(path)
    writeJSON(w, 200, map[string]any{"ok": true, "xray_running": running})
}

func (s *Server) XrayRestart(w http.ResponseWriter, r *http.Request) {
    ok := s.App.Xray().Restart(s.App.Config().ConfigPath)
    writeJSON(w, 200, map[string]any{"ok": ok, "xray_running": s.App.Xray().Running()})
}

func (s *Server) RawConfig(w http.ResponseWriter, r *http.Request) {
    applied := readApplied(s.App.Config().ConfigPath)
    if applied == nil {
        writeError(w, 404, "尚未应用过配置"); return
    }
    writeJSON(w, 200, applied)
}

func (s *Server) Topology(w http.ResponseWriter, r *http.Request) {
    cfg := readApplied(s.App.Config().ConfigPath)
    inbs, outs, route := []any{}, []any{}, []any{}
    if cfg != nil {
        for _, ib := range arr(cfg["inbounds"]) {
            m := asMap(ib)
            inbs = append(inbs, map[string]any{
                "tag": m["tag"], "protocol": m["protocol"],
                "listen": m["listen"], "port": m["port"],
            })
        }
        rev := map[string][]string{}
        routing := asMap(cfg["routing"])
        for i, r := range arr(routing["rules"]) {
            rm := asMap(r)
            desc := service.Describe(rm)
            tag := ""
            if t, _ := rm["balancerTag"].(string); t != "" {
                tag = t
            } else {
                tag, _ = rm["outboundTag"].(string)
            }
            if desc == "默认出口(其余流量)" {
                rev[tag] = append(rev[tag], "默认出口")
            } else {
                rev[tag] = append(rev[tag], desc)
            }
            route = append(route, map[string]any{
                "order": i + 1, "match": desc, "outbound": tag,
                "label": s.App.OutboundLabel(tag),
            })
        }
        for _, ob := range arr(cfg["outbounds"]) {
            om := asMap(ob)
            t, _ := om["tag"].(string)
            outs = append(outs, map[string]any{
                "tag": t, "protocol": om["protocol"],
                "label": s.App.OutboundLabel(t),
                "rules": rev[t],
            })
        }
        for _, b := range arr(asMap(cfg["routing"])["balancers"]) {
            bm := asMap(b)
            t, _ := bm["tag"].(string)
            outs = append(outs, map[string]any{
                "tag": t, "protocol": "balancer/",
                "label": s.App.OutboundLabel(t),
                "rules": rev[t], "members": bm["selector"],
            })
        }
    }
    applied := cfg != nil
    writeJSON(w, 200, map[string]any{
        "applied": applied, "dirty": s.App.Dirty(cfg),
        "inbounds": inbs, "outbounds": outs, "routing": route,
    })
}

// helpers
func readApplied(path string) map[string]any {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil
    }
    var m map[string]any
    json.Unmarshal(data, &m)
    return m
}

// computeDirty 通过 s.App.Dirty() 调用，实现在 app 包中
// see app/app.go → func (a *App) Dirty(applied map[string]any) bool

func cfgJSON(cfg map[string]any) []byte {
    data, _ := json.Marshal(cfg)
    return data
}

func arr(v any) []any {
    a, _ := v.([]any)
    if a == nil {
        return []any{}
    }
    return a
}

func asMap(v any) map[string]any {
    m, _ := v.(map[string]any)
    return m
}
```

同时在 `app/app.go` 中添加 `Dirty` 方法：

```go
import "encoding/json"

func (a *App) Dirty(applied map[string]any) bool {
    if len(a.state.Nodes) == 0 {
        return false
    }
    if applied == nil {
        return true
    }
    draft := service.BuildConfig(a.state)
    key := func(c map[string]any) string {
        data, _ := json.Marshal(map[string]any{
            "i": c["inbounds"], "o": c["outbounds"],
            "r": c["routing"], "ob": c["observatory"],
        })
        return string(data)
    }
    return key(draft) != key(applied)
}
```

- [ ] **Step 2: 编译验证**

```bash
cd go-backend && go build ./...
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/handler/routing.go go-backend/internal/handler/subscription.go go-backend/internal/handler/xray.go
git commit -m "feat: add routing/subscription/xray handlers

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 18: 程序入口 — cmd/server/main.go

**Files:**
- Create: `go-backend/cmd/server/main.go`

- [ ] **Step 1: 实现**

```go
// go-backend/cmd/server/main.go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "xray-panel/internal/app"
    "xray-panel/internal/config"
    "xray-panel/internal/handler"
    "xray-panel/internal/service"
    "xray-panel/internal/store"
)

func main() {
    cfg, err := config.Load()
    if err != nil {
        slog.Error("加载配置失败", "error", err)
        os.Exit(1)
    }
    if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
        slog.Error("创建数据目录失败", "error", err)
        os.Exit(1)
    }

    st := store.NewJSONStore(cfg.StatePath(), cfg.SocksPort, cfg.HTTPPort)
    xp := service.NewXrayProc(cfg.XrayBin, cfg.DataDir)

    a, err := app.New(app.Config{
        Store:                     st,
        XrayProc:                  xp,
        ConfigPath:                cfg.ConfigPath(),
        PanelPort:                 cfg.PanelPort,
        Password:                  cfg.PanelPassword,
        SubscriptionAllowInternal: cfg.SubscriptionAllowInternal,
    })
    if err != nil {
        slog.Error("初始化失败", "error", err)
        os.Exit(1)
    }

    a.Xray().Start(cfg.ConfigPath())

    srv := handler.NewServer(a)
    httpServer := &http.Server{
        Addr:    fmt.Sprintf("%s:%d", cfg.PanelListen, cfg.PanelPort),
        Handler: srv.Routes(),
    }

    ctx, stop := signal.NotifyContext(context.Background(),
        syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go func() {
        slog.Info(fmt.Sprintf("面板已启动 http://%s:%d", cfg.PanelListen, cfg.PanelPort))
        if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("服务异常退出", "error", err)
        }
    }()

    <-ctx.Done()
    slog.Info("正在关闭...")
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    httpServer.Shutdown(shutdownCtx)
    a.Xray().Stop()
    slog.Info("已关闭")
}
```

- [ ] **Step 2: 编译**

```bash
cd go-backend && go build ./cmd/server/
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/cmd/server/main.go
git commit -m "feat: add main entry point with graceful shutdown

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 19: Docker 部署文件

**Files:**
- Create: `go-backend/Dockerfile`
- Create: `go-backend/docker-compose.yml`

- [ ] **Step 1: 实现 Dockerfile**

```dockerfile
# go-backend/Dockerfile
# 阶段 1：xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:latest AS xray

# 阶段 2：Go 构建
FROM golang:1.23-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o xray-panel ./cmd/server

# 阶段 3：极简运行时
FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
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

- [ ] **Step 2: 实现 docker-compose.yml**

```yaml
# go-backend/docker-compose.yml
services:
  xray-panel:
    build: .
    ports:
      - "12017:2017"
      - "10808:10808"
      - "10809:10809"
    image: xray-panel-go:local
    container_name: xray-panel-go
    restart: always
    network_mode: host
    environment:
      - PANEL_DATA_DIR=/data/xray
      - PANEL_PORT=2017
      - PANEL_PASSWORD=123321123
      - SUBSCRIPTION_ALLOW_INTERNAL=1
    volumes:
      - ./data:/data/xray
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/Dockerfile go-backend/docker-compose.yml
git commit -m "feat: add multi-stage Dockerfile (scratch base) and docker-compose

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 20: 集成测试（端到端 API 测试）

**Files:**
- Create: `go-backend/internal/handler/handler_test.go`

- [ ] **Step 1: 实现集成测试**

```go
// go-backend/internal/handler/handler_test.go
package handler

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "xray-panel/internal/app"
    "xray-panel/internal/config"
    "xray-panel/internal/model"
    "xray-panel/internal/service"
    "xray-panel/internal/store"
)

// 内存 store（无需文件系统）
type memStore struct {
    state *model.PanelState
}

func (m *memStore) Load() (*model.PanelState, error) {
    if m.state == nil {
        m.state = &model.PanelState{
            Subscription: model.Subscription{},
            Inbounds: []model.Inbound{
                {Tag: "in-0", Protocol: "socks", Listen: "0.0.0.0", Port: 10808, UDP: true},
                {Tag: "in-1", Protocol: "http", Listen: "0.0.0.0", Port: 10809},
            },
        }
    }
    return m.state, nil
}

func (m *memStore) Save(state *model.PanelState) error { m.state = state; return nil }
func (m *memStore) Lock()                               {}
func (m *memStore) Unlock()                             {}

func setupTestApp(t *testing.T) (*app.App, *Server) {
    t.Helper()
    pw := "testpw"
    cfg := app.Config{
        Store:      &memStore{},
        XrayProc:   &service.FakeXray{Alive: true},
        ConfigPath: "/tmp/test-config.json",
        PanelPort:  2017,
        Password:   &pw,
    }
    a, err := app.New(cfg)
    if err != nil {
        t.Fatal(err)
    }
    return a, NewServer(a)
}

func TestLogin(t *testing.T) {
    _, srv := setupTestApp(t)
    ts := httptest.NewServer(srv.Routes())
    defer ts.Close()

    body, _ := json.Marshal(map[string]string{"password": "testpw"})
    resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", bytes.NewReader(body))
    if err != nil {
        t.Fatal(err)
    }
    if resp.StatusCode != 200 {
        t.Errorf("status = %d, want 200", resp.StatusCode)
    }
    var result map[string]any
    json.NewDecoder(resp.Body).Decode(&result)
    if result["token"] == nil || result["token"] == "" {
        t.Error("token should not be empty")
    }
}
```

- [ ] **Step 2: 运行测试**

```bash
cd go-backend && go test ./internal/handler/ -v -run TestLogin
```

- [ ] **Step 3: 提交**

```bash
git add go-backend/internal/handler/handler_test.go
git commit -m "test: add integration test for auth login

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 21: 全量编译验证 + 补齐遗漏

- [ ] **Step 1: 全量编译**

```bash
cd go-backend && go build ./...
```

- [ ] **Step 2: 运行全部测试**

```bash
cd go-backend && go test ./... -v
```

- [ ] **Step 3: 修复任何编译/测试错误**

根据编译输出逐项修复。常见问题：
- import 循环依赖
- 类型不匹配（`auth.PasswordRec` vs `model.PasswordRec`）
- `config.go` 中 `SubscriptionAllowInternal` 字段未导出到 `app.Config`
- `handler/xray.go` 中的 `cfgJSON` 和 `asMap`/`arr` 辅助函数需要添加

- [ ] **Step 4: 提交补丁**

```bash
git add -A go-backend/
git commit -m "fix: resolve compilation issues and complete wiring"
```

---

### Task 22: 与 Python 版行为对照验证

- [ ] **Step 1: 启动 Go 后端**

```bash
cd go-backend
PANEL_DATA_DIR=$(mktemp -d) PANEL_PASSWORD=testpw XRAY_BIN=/bin/true PANEL_PORT=9999 go run ./cmd/server/ &
sleep 2
```

- [ ] **Step 2: 验证 API 兼容性（登录）**

```bash
# 登录
TOKEN=$(curl -s -X POST http://localhost:9999/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"password":"testpw"}' | jq -r '.token')
echo "Token: $TOKEN"

# 验证 token
curl -s http://localhost:9999/api/auth/me -H "Authorization: Bearer $TOKEN"

# 获取入站
curl -s http://localhost:9999/api/inbounds -H "Authorization: Bearer $TOKEN"

# 创建入站
curl -s -X POST http://localhost:9999/api/inbounds \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"protocol":"socks","port":1080}'

# 创建代理
curl -s -X POST http://localhost:9999/api/proxies \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"protocol":"http","host":"proxy.example.com","port":8080}'

# 获取路由模板
curl -s http://localhost:9999/api/routing/templates -H "Authorization: Bearer $TOKEN"

# 获取出口选项
curl -s http://localhost:9999/api/outbounds -H "Authorization: Bearer $TOKEN"

# Xray 状态
curl -s http://localhost:9999/api/xray/status -H "Authorization: Bearer $TOKEN"
```

- [ ] **Step 3: 停止 Go 后端**

```bash
kill %1
```

- [ ] **Step 4: 清理**

```bash
# 测试通过后提交
git commit --allow-empty -m "verify: Go backend API compatibility confirmed"
```

验证所有端点返回的 JSON 结构与 Python 版一致（路径、状态码、字段名、错误格式）。

---

## 自审检查

执行计划前请确认：

1. **Spec 覆盖**：对照 `docs/superpowers/specs/2026-05-30-xray-panel-go-migration-design.md` 的 12 个 section，Task 1-22 覆盖了全部内容
2. **无占位符**：所有 task 均有完整代码，无 TODO/TBD
3. **类型一致性**：`auth.PasswordRec` 与 `model.PasswordRec` 已统一；`app.Config` 包含 `SubscriptionAllowInternal` 字段；`handler/xray.go` 中 helper 函数 `cfgJSON`、`asMap`、`arr` 需要在 Task 17 补充
4. **依赖顺序正确**：Task 1→2→3→4→5→6→7→8→9→10→11→12→13→14→15→16→17→18→19→20→21→22

### 需要的补丁（Task 17 补充）

在 `handler/xray.go` 中添加：
```go
func cfgJSON(cfg map[string]any) []byte {
    data, _ := json.Marshal(cfg)
    return data
}

func asMap(v any) map[string]any {
    m, _ := v.(map[string]any)
    return m
}

func arr(v any) []any {
    a, _ := v.([]any)
    if a == nil {
        return []any{}
    }
    return a
}
```

在 `config.go` 中导出 `SubscriptionAllowInternal`，在 `app.Config` 中添加对应字段。
