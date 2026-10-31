# Xray 面板后端 FastAPI 重构 Implementation Plan (Plan 1 of 2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Xray 面板后端从标准库 `http.server` 重构为 FastAPI + Pydantic + Bearer 鉴权的 RESTful 服务,复用现有纯逻辑模块,保持 `panel.json` 持久化与两段式应用流程。

**Architecture:** FastAPI 单应用,运行时单例 `AppState`(持有 Pydantic `PanelState` + 全局锁 + `Xray` 进程句柄)经依赖注入给各资源路由。现有纯逻辑(`config_builder`/`routing`/`xray_sub`/`xray_proc`/`inbounds`/`proxies`)迁入 `backend/services/` 基本原样复用。各资源用 RESTful CRUD;路由规则整体 PUT(保序);`POST /api/apply` 才构建配置、`xray -test` 校验、落盘并重启 Xray。鉴权用不透明随机 token + 内存会话表,经 `HTTPBearer` 依赖保护除登录外所有 `/api/*`。

**Tech Stack:** Python 3.12, FastAPI, Uvicorn, Pydantic v2, pydantic-settings, pytest + Starlette `TestClient`,uv 管理依赖。

**参考 spec:** `docs/superpowers/specs/2026-05-30-xray-panel-refactor-design.md`

---

## 文件结构

| 文件 | 职责 |
|------|------|
| `pyproject.toml` | 依赖声明(运行时 + dev) |
| `backend/__init__.py` | 包标记 |
| `backend/config.py` | `Settings`(pydantic-settings),读环境变量 |
| `backend/state.py` | Pydantic 状态模型 `PanelState` 及子模型 + 迁移 + 原子读写 |
| `backend/security.py` | 密码哈希/校验 + 会话表 + `require_auth` 依赖(HTTPBearer) |
| `backend/schemas.py` | 请求体 Pydantic 模型 + 字段校验 |
| `backend/deps.py` | 运行时单例 `AppState`(state+lock+xray) + 获取依赖 |
| `backend/main.py` | app 工厂、挂载路由、静态托管、启动钩子 |
| `backend/routers/auth.py` | 登录/登出/me |
| `backend/routers/inbounds.py` | 入站 CRUD |
| `backend/routers/proxies.py` | 落地代理 CRUD |
| `backend/routers/balancers.py` | 自动组 CRUD |
| `backend/routers/subscription.py` | 订阅 PUT/GET、节点 GET、测速 |
| `backend/routers/routing.py` | 规则+默认出口 GET/PUT、模板、出口选项聚合 |
| `backend/routers/xray.py` | 应用/重启/状态/原始配置/拓扑 |
| `backend/services/*.py` | 现有纯逻辑模块迁入 |
| `tests/*.py` | pytest 测试 |
| `Dockerfile` / `docker-compose.yml` | 多阶段构建 / 部署 |

**约定**:入站/代理/自动组用持久 `seq` 计数器生成稳定 tag(`in-0`、`px-0`、`auto-0`),CRUD 按 tag 操作,删除不重排其它资源 tag(REST 稳定 id)。节点 tag 仍在订阅拉取时整体重排(`node-0..`,沿用现状)。

---

## Task 1: 脚手架 + 依赖 + 迁移 service 模块

**Files:**
- Modify: `pyproject.toml`
- Create: `backend/__init__.py`, `backend/services/__init__.py`, `tests/__init__.py`
- Move: `routing.py` `config_builder.py` `inbounds.py` `proxies.py` `xray_sub.py` `xray_proc.py` → `backend/services/`
- Modify: `backend/services/config_builder.py`(改 import)
- Test: `tests/test_services_smoke.py`

- [ ] **Step 1: 更新 pyproject.toml 依赖**

```toml
[project]
name = "xray-panel"
version = "0.2.0"
description = "Professional Xray management panel"
readme = "README.md"
requires-python = ">=3.12"
dependencies = [
    "fastapi>=0.115",
    "uvicorn[standard]>=0.30",
    "pydantic>=2.7",
    "pydantic-settings>=2.3",
]

[dependency-groups]
dev = [
    "pytest>=8.0",
    "httpx>=0.27",
]

[tool.pytest.ini_options]
pythonpath = ["."]
testpaths = ["tests"]
```

- [ ] **Step 2: 同步依赖,创建包文件**

```bash
uv sync
mkdir -p backend/services backend/routers tests
touch backend/__init__.py backend/services/__init__.py backend/routers/__init__.py tests/__init__.py
```

- [ ] **Step 3: 写迁移后导入的失败测试**

`tests/test_services_smoke.py`:

```python
def test_build_config_minimal():
    from backend.services import config_builder
    state = {
        "nodes": [{"tag": "node-0", "name": "n", "type": "vmess",
                   "host": "h", "port": 1, "outbound": {"protocol": "vmess"}}],
        "inbounds": [{"tag": "in-0", "protocol": "socks", "listen": "127.0.0.1",
                      "port": 10808, "udp": True, "auth": None}],
        "proxies": [], "balancers": [], "rules": [], "default_outbound": "node-0",
    }
    cfg = config_builder.build_config(state)
    assert cfg["outbounds"][0]["tag"] == "node-0"
    assert any(o["tag"] == "direct" for o in cfg["outbounds"])
    assert cfg["routing"]["rules"][-1]["outboundTag"] == "node-0"


def test_parse_vmess_roundtrip():
    from backend.services import xray_sub
    import base64, json
    raw = base64.b64encode(json.dumps(
        {"add": "1.2.3.4", "port": "443", "id": "uuid", "ps": "节点A"}
    ).encode()).decode()
    node = xray_sub.parse_vmess("vmess://" + raw)
    assert node["host"] == "1.2.3.4" and node["port"] == 443
```

- [ ] **Step 4: 运行测试确认失败**

Run: `uv run pytest tests/test_services_smoke.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'backend.services.config_builder'`

- [ ] **Step 5: 用 git mv 迁移模块**

```bash
git mv routing.py backend/services/routing.py
git mv config_builder.py backend/services/config_builder.py
git mv inbounds.py backend/services/inbounds.py
git mv proxies.py backend/services/proxies.py
git mv xray_sub.py backend/services/xray_sub.py
git mv xray_proc.py backend/services/xray_proc.py
```

- [ ] **Step 6: 修正 config_builder 的相对导入**

`backend/services/config_builder.py` 顶部三行原为 `import routing` / `import inbounds` / `import proxies`,改为:

```python
"""config_builder.py — 由 state 生成完整 Xray config(纯函数)。"""
import copy
from . import routing, inbounds, proxies
```

(其余函数体不变。)

- [ ] **Step 7: 运行测试确认通过**

Run: `uv run pytest tests/test_services_smoke.py -v`
Expected: PASS(2 passed)

- [ ] **Step 8: 提交**

```bash
git add -A
git commit -m "refactor: 迁移纯逻辑模块到 backend/services 并加冒烟测试"
```

---

## Task 2: 配置 Settings

**Files:**
- Create: `backend/config.py`
- Test: `tests/test_config.py`

- [ ] **Step 1: 写失败测试**

`tests/test_config.py`:

```python
def test_defaults(monkeypatch):
    for k in ("PANEL_PORT", "PANEL_DATA_DIR", "XRAY_BIN", "PANEL_PASSWORD"):
        monkeypatch.delenv(k, raising=False)
    from backend.config import Settings
    s = Settings()
    assert s.panel_port == 2017
    assert s.data_dir == "/data/xray"
    assert s.state_path.endswith("panel.json")
    assert s.config_path.endswith("config.json")


def test_env_override(monkeypatch):
    monkeypatch.setenv("PANEL_PORT", "3000")
    monkeypatch.setenv("PANEL_DATA_DIR", "/tmp/x")
    from backend.config import Settings
    s = Settings()
    assert s.panel_port == 3000
    assert s.state_path == "/tmp/x/panel.json"
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_config.py -v`
Expected: FAIL — `No module named 'backend.config'`

- [ ] **Step 3: 实现 config.py**

```python
"""config.py — 面板设置,从环境变量读取(pydantic-settings)。"""
import os
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="", extra="ignore")

    panel_port: int = 2017
    panel_listen: str = "0.0.0.0"
    data_dir: str = "/data/xray"
    xray_bin: str = "/usr/local/bin/xray"
    panel_password: str | None = None
    socks_port: int = 10808            # 仅旧数据迁移时播种默认入站用
    http_port: int = 10809

    @property
    def state_path(self) -> str:
        return os.path.join(self.data_dir, "panel.json")

    @property
    def config_path(self) -> str:
        return os.path.join(self.data_dir, "config.json")
```

pydantic-settings 默认大小写不敏感地匹配字段名,故 `PANEL_PORT` → `panel_port`。

- [ ] **Step 4: 运行确认通过**

Run: `uv run pytest tests/test_config.py -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/config.py tests/test_config.py
git commit -m "feat: 添加 Settings 配置(pydantic-settings)"
```

---

## Task 3: 状态模型与持久化

**Files:**
- Create: `backend/state.py`
- Test: `tests/test_state.py`

- [ ] **Step 1: 写失败测试**

`tests/test_state.py`:

```python
import json
from backend.state import PanelState, load, save, migrate


def test_load_missing_returns_defaults(tmp_path):
    st = load(str(tmp_path / "none.json"))
    assert st.nodes == [] and st.rules == []
    assert st.inbounds and st.inbounds[0].protocol == "socks"   # 播种默认入站


def test_migrate_old_data_seeds_inbounds_and_rule_ids():
    old = {"nodes": [], "rules": [{"type": "full", "value": "a.com", "outbound": "direct"}]}
    st = migrate(old)
    assert st.inbounds and len(st.inbounds) == 2                 # 老数据无 inbounds → 播种
    assert st.rules[0].id == 1 and st.rules[0].enabled is True   # 补 id/enabled


def test_seq_counters_initialised_from_existing_tags():
    st = migrate({"inbounds": [{"tag": "in-0", "protocol": "http", "listen": "127.0.0.1",
                                "port": 8080, "auth": None}]})
    assert st.inbound_seq == 1                                   # max(0)+1


def test_save_load_roundtrip(tmp_path):
    p = str(tmp_path / "panel.json")
    st = migrate({"default_outbound": "node-0"})
    save(p, st)
    again = load(p)
    assert again.default_outbound == "node-0"
    assert json.loads(open(p).read())["default_outbound"] == "node-0"
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_state.py -v`
Expected: FAIL — `No module named 'backend.state'`

- [ ] **Step 3: 实现 state.py**

```python
"""state.py — panel.json 的 Pydantic 模型、迁移、原子读写。"""
import json
import os
from typing import Any, Literal, Optional
from pydantic import BaseModel, Field


class Auth(BaseModel):
    user: str
    password: str = Field(alias="pass")
    model_config = {"populate_by_name": True}

    def dump(self) -> dict:
        return {"user": self.user, "pass": self.password}


class PasswordRec(BaseModel):
    salt: str
    hash: str


class Subscription(BaseModel):
    url: str = ""
    remarks: str = ""
    status: str = ""
    fetched_at: int = 0


class Node(BaseModel):
    tag: str
    name: str
    type: str
    host: str
    port: int
    latency: Optional[int] = None
    outbound: dict[str, Any]


class Inbound(BaseModel):
    tag: str
    protocol: Literal["socks", "http"]
    listen: str = "127.0.0.1"
    port: int
    udp: bool = True
    auth: Optional[Auth] = None


class Proxy(BaseModel):
    tag: str
    name: str
    protocol: Literal["socks", "http"]
    host: str
    port: int
    auth: Optional[Auth] = None


class Balancer(BaseModel):
    tag: str
    name: str
    nodes: list[str]
    strategy: str = "leastPing"


class Rule(BaseModel):
    id: int
    type: Literal["domain-suffix", "full", "keyword", "geosite", "ip", "geoip", "port"]
    value: str
    outbound: str
    enabled: bool = True


class PanelState(BaseModel):
    password: Optional[PasswordRec] = None
    subscription: Subscription = Subscription()
    nodes: list[Node] = []
    inbounds: list[Inbound] = []
    proxies: list[Proxy] = []
    balancers: list[Balancer] = []
    rules: list[Rule] = []
    default_outbound: str = ""
    inbound_seq: int = 0
    proxy_seq: int = 0
    balancer_seq: int = 0


def _default_inbounds(socks_port: int = 10808, http_port: int = 10809) -> list[dict]:
    return [
        {"tag": "in-0", "protocol": "socks", "listen": "0.0.0.0", "port": socks_port,
         "udp": True, "auth": None},
        {"tag": "in-1", "protocol": "http", "listen": "0.0.0.0", "port": http_port,
         "auth": None},
    ]


def _seq_from_tags(items: list[dict], prefix: str) -> int:
    mx = -1
    for it in items:
        tag = it.get("tag", "")
        if tag.startswith(prefix):
            try:
                mx = max(mx, int(tag[len(prefix):]))
            except ValueError:
                pass
    return mx + 1


def migrate(raw: Optional[dict], socks_port: int = 10808, http_port: int = 10809) -> PanelState:
    """把任意旧 panel.json dict 规整成 PanelState(向后兼容)。"""
    raw = dict(raw or {})
    if "inbounds" not in raw:                       # 老数据无此字段 → 播种默认入站
        raw["inbounds"] = _default_inbounds(socks_port, http_port)
    raw.setdefault("proxies", [])
    raw.setdefault("balancers", [])
    # 规则补 id / enabled
    next_id, fixed = 1, []
    for r in raw.get("rules", []):
        r = dict(r)
        r.setdefault("id", next_id)
        r.setdefault("enabled", True)
        next_id = max(next_id, r["id"]) + 1
        fixed.append(r)
    raw["rules"] = fixed
    # seq 计数器:已存在则保留,否则由现有 tag 推断
    raw.setdefault("inbound_seq", _seq_from_tags(raw["inbounds"], "in-"))
    raw.setdefault("proxy_seq", _seq_from_tags(raw["proxies"], "px-"))
    raw.setdefault("balancer_seq", _seq_from_tags(raw["balancers"], "auto-"))
    return PanelState.model_validate(raw)


def load(path: str, socks_port: int = 10808, http_port: int = 10809) -> PanelState:
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            return migrate(json.load(f), socks_port, http_port)
    return migrate({}, socks_port, http_port)


def save(path: str, state: PanelState) -> None:
    d = os.path.dirname(path)
    if d:
        os.makedirs(d, exist_ok=True)
    tmp = path + ".tmp"
    data = state.model_dump(by_alias=True)
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
    os.replace(tmp, path)
```

注意:`Auth` 用 alias `pass`(Python 关键字),`model_dump(by_alias=True)` 输出 `{"user","pass"}`,与现有 panel.json 及 `to_xray` 期望一致。

- [ ] **Step 4: 运行确认通过**

Run: `uv run pytest tests/test_state.py -v`
Expected: PASS(4 passed)

- [ ] **Step 5: 提交**

```bash
git add backend/state.py tests/test_state.py
git commit -m "feat: PanelState Pydantic 模型 + 迁移 + 原子读写"
```

---

## Task 4: 鉴权(密码 + 会话 + 依赖)

**Files:**
- Create: `backend/security.py`
- Test: `tests/test_security.py`

- [ ] **Step 1: 写失败测试**

`tests/test_security.py`:

```python
import pytest
from backend import security


def test_hash_and_verify():
    rec = security.hash_password("s3cret")
    assert set(rec) == {"salt", "hash"}
    assert security.verify_password(rec, "s3cret")
    assert not security.verify_password(rec, "wrong")
    assert not security.verify_password(None, "s3cret")


def test_token_lifecycle():
    store = security.SessionStore(ttl=1000)
    tok = store.create()
    assert store.valid(tok)
    store.revoke(tok)
    assert not store.valid(tok)


def test_token_expiry():
    store = security.SessionStore(ttl=-1)        # 立即过期
    tok = store.create()
    assert not store.valid(tok)


def test_require_auth_rejects_missing(monkeypatch):
    from fastapi import HTTPException
    store = security.SessionStore()
    with pytest.raises(HTTPException) as ei:
        security._check_token(store, None)
    assert ei.value.status_code == 401
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_security.py -v`
Expected: FAIL — `No module named 'backend.security'`

- [ ] **Step 3: 实现 security.py**

```python
"""security.py — 密码哈希、Bearer 会话、auth 依赖。"""
import hashlib
import secrets
import time
from typing import Optional
from fastapi import Depends, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer


def hash_password(pw: str, salt: Optional[str] = None) -> dict:
    salt = salt or secrets.token_hex(16)
    h = hashlib.pbkdf2_hmac("sha256", pw.encode(), bytes.fromhex(salt), 200_000).hex()
    return {"salt": salt, "hash": h}


def verify_password(rec: Optional[dict], pw: str) -> bool:
    if not rec:
        return False
    calc = hash_password(pw, rec["salt"])["hash"]
    return secrets.compare_digest(calc, rec["hash"])


class SessionStore:
    """内存 token→过期时间 会话表。"""
    def __init__(self, ttl: int = 7 * 86400):
        self.ttl = ttl
        self._tokens: dict[str, float] = {}

    def create(self) -> str:
        now = time.time()
        for t in [t for t, exp in self._tokens.items() if exp <= now]:
            self._tokens.pop(t, None)
        tok = secrets.token_hex(24)
        self._tokens[tok] = now + self.ttl
        return tok

    def valid(self, token: Optional[str]) -> bool:
        exp = self._tokens.get(token or "")
        if exp and exp > time.time():
            return True
        self._tokens.pop(token or "", None)
        return False

    def revoke(self, token: Optional[str]) -> None:
        self._tokens.pop(token or "", None)


_bearer = HTTPBearer(auto_error=False)


def _check_token(store: SessionStore, token: Optional[str]) -> None:
    if not store.valid(token):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED,
                            detail="未授权或登录已过期")


def make_auth_dependency(get_store):
    """返回一个 FastAPI 依赖:校验 Authorization: Bearer <token>。"""
    def require_auth(cred: Optional[HTTPAuthorizationCredentials] = Depends(_bearer),
                     store: SessionStore = Depends(get_store)) -> None:
        token = cred.credentials if cred else None
        _check_token(store, token)
    return require_auth
```

- [ ] **Step 4: 运行确认通过**

Run: `uv run pytest tests/test_security.py -v`
Expected: PASS(4 passed)

- [ ] **Step 5: 提交**

```bash
git add backend/security.py tests/test_security.py
git commit -m "feat: Bearer 鉴权(密码哈希 + 会话表 + 依赖)"
```

---

## Task 5: 请求体 Schemas

**Files:**
- Create: `backend/schemas.py`
- Test: `tests/test_schemas.py`

- [ ] **Step 1: 写失败测试**

`tests/test_schemas.py`:

```python
import pytest
from pydantic import ValidationError
from backend import schemas


def test_inbound_valid():
    ib = schemas.InboundIn(protocol="socks", listen="127.0.0.1", port=10808, udp=True)
    assert ib.port == 10808


@pytest.mark.parametrize("port", [0, 70000, -1])
def test_inbound_bad_port(port):
    with pytest.raises(ValidationError):
        schemas.InboundIn(protocol="socks", port=port)


def test_inbound_bad_protocol():
    with pytest.raises(ValidationError):
        schemas.InboundIn(protocol="vmess", port=1080)


def test_auth_requires_both():
    with pytest.raises(ValidationError):
        schemas.AuthIn(user="u", **{"pass": ""})


def test_rule_unknown_type():
    with pytest.raises(ValidationError):
        schemas.RuleIn(type="bogus", value="x", outbound="direct")
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_schemas.py -v`
Expected: FAIL — `No module named 'backend.schemas'`

- [ ] **Step 3: 实现 schemas.py**

```python
"""schemas.py — API 请求体模型与字段校验。"""
from typing import Literal, Optional
from pydantic import BaseModel, Field, field_validator, model_validator


class LoginIn(BaseModel):
    password: str


class AuthIn(BaseModel):
    user: str
    password: str = Field(alias="pass")
    model_config = {"populate_by_name": True}

    @model_validator(mode="after")
    def both_or_neither(self):
        if not self.user.strip() or not self.password:
            raise ValueError("鉴权的账号和密码都不能为空")
        return self


class InboundIn(BaseModel):
    protocol: Literal["socks", "http"]
    listen: str = "127.0.0.1"
    port: int
    udp: bool = True
    auth: Optional[AuthIn] = None

    @field_validator("port")
    @classmethod
    def port_range(cls, v):
        if not (1 <= v <= 65535):
            raise ValueError(f"端口 {v} 超出范围(1–65535)")
        return v


class ProxyIn(BaseModel):
    name: str = ""
    protocol: Literal["socks", "http"]
    host: str
    port: int
    auth: Optional[AuthIn] = None

    @field_validator("host")
    @classmethod
    def host_nonempty(cls, v):
        if not v.strip():
            raise ValueError("代理地址(host)不能为空")
        return v.strip()

    @field_validator("port")
    @classmethod
    def port_range(cls, v):
        if not (1 <= v <= 65535):
            raise ValueError(f"端口 {v} 超出范围(1–65535)")
        return v


class BalancerIn(BaseModel):
    name: str = ""
    nodes: list[str]


class RuleIn(BaseModel):
    id: Optional[int] = None
    type: Literal["domain-suffix", "full", "keyword", "geosite", "ip", "geoip", "port"]
    value: str = ""
    outbound: str
    enabled: bool = True


class RoutingIn(BaseModel):
    default_outbound: str = ""
    rules: list[RuleIn] = []


class SubscriptionIn(BaseModel):
    url: str
```

- [ ] **Step 4: 运行确认通过**

Run: `uv run pytest tests/test_schemas.py -v`
Expected: PASS(7 passed,含参数化)

- [ ] **Step 5: 提交**

```bash
git add backend/schemas.py tests/test_schemas.py
git commit -m "feat: API 请求体 Pydantic 模型 + 校验"
```

---

## Task 6: 运行时 AppState 与依赖

**Files:**
- Create: `backend/deps.py`
- Test: `tests/test_deps.py`

- [ ] **Step 1: 写失败测试**

`tests/test_deps.py`:

```python
from backend.deps import AppState


def test_appstate_outbound_tags(tmp_path):
    app_state = AppState(state_path=str(tmp_path / "p.json"),
                         config_path=str(tmp_path / "c.json"),
                         xray_bin="/bin/true")
    app_state.state.nodes = []
    tags = app_state.outbound_tags()
    assert "direct" in tags and "block" in tags


def test_appstate_persist(tmp_path):
    p = str(tmp_path / "p.json")
    app_state = AppState(state_path=p, config_path=str(tmp_path / "c.json"),
                         xray_bin="/bin/true")
    app_state.state.default_outbound = "direct"
    app_state.persist()
    import json
    assert json.loads(open(p).read())["default_outbound"] == "direct"
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_deps.py -v`
Expected: FAIL — `No module named 'backend.deps'`

- [ ] **Step 3: 实现 deps.py**

```python
"""deps.py — 运行时单例 AppState(状态 + 锁 + Xray 进程)及 FastAPI 依赖。"""
import threading
from backend import state as state_mod
from backend.security import SessionStore
from backend.services.xray_proc import Xray


class AppState:
    def __init__(self, state_path: str, config_path: str, xray_bin: str,
                 data_dir: str = None, socks_port: int = 10808, http_port: int = 10809):
        import os
        self.state_path = state_path
        self.config_path = config_path
        self.data_dir = data_dir or os.path.dirname(state_path)
        self.lock = threading.Lock()
        self.sessions = SessionStore()
        self.state = state_mod.load(state_path, socks_port, http_port)
        self.xray = Xray(bin=xray_bin, workdir=self.data_dir)

    def persist(self) -> None:
        state_mod.save(self.state_path, self.state)

    def balancer_tags(self) -> set[str]:
        return {b.tag for b in self.state.balancers}

    def outbound_tags(self) -> set[str]:
        s = self.state
        return ({n.tag for n in s.nodes} | {b.tag for b in s.balancers}
                | {p.tag for p in s.proxies} | {"direct", "block"})

    def outbound_label(self, tag: str) -> str:
        if tag == "direct":
            return "直连"
        if tag == "block":
            return "阻断"
        for n in self.state.nodes:
            if n.tag == tag:
                return n.name
        for b in self.state.balancers:
            if b.tag == tag:
                return f"⚖ {b.name}(自动)"
        for p in self.state.proxies:
            if p.tag == tag:
                return f"🛰 {p.name}(落地)"
        return tag


# 由 main.py 在启动时赋值的单例
_app_state: AppState | None = None


def set_app_state(s: AppState) -> None:
    global _app_state
    _app_state = s


def get_app_state() -> AppState:
    assert _app_state is not None, "AppState 未初始化"
    return _app_state


def get_sessions() -> SessionStore:
    return get_app_state().sessions
```

- [ ] **Step 4: 运行确认通过**

Run: `uv run pytest tests/test_deps.py -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/deps.py tests/test_deps.py
git commit -m "feat: 运行时 AppState 单例 + 依赖"
```

---

## Task 7: app 工厂 + 认证路由 + 静态托管

**Files:**
- Create: `backend/main.py`, `backend/routers/auth.py`
- Test: `tests/conftest.py`, `tests/test_auth_api.py`

- [ ] **Step 1: 写测试夹具 + 失败测试**

`tests/conftest.py`:

```python
import pytest
from fastapi.testclient import TestClient


@pytest.fixture
def client(tmp_path, monkeypatch):
    monkeypatch.setenv("PANEL_DATA_DIR", str(tmp_path))
    monkeypatch.setenv("PANEL_PASSWORD", "testpw")
    monkeypatch.setenv("XRAY_BIN", "/bin/true")
    from backend.main import create_app
    app = create_app(start_xray=False)
    return TestClient(app)


@pytest.fixture
def auth_client(client):
    r = client.post("/api/auth/login", json={"password": "testpw"})
    assert r.status_code == 200
    token = r.json()["token"]
    client.headers.update({"Authorization": f"Bearer {token}"})
    return client
```

`tests/test_auth_api.py`:

```python
def test_login_wrong_password(client):
    r = client.post("/api/auth/login", json={"password": "nope"})
    assert r.status_code == 401


def test_login_ok_returns_token(client):
    r = client.post("/api/auth/login", json={"password": "testpw"})
    assert r.status_code == 200 and r.json()["token"]


def test_protected_requires_token(client):
    assert client.get("/api/inbounds").status_code == 401


def test_protected_with_token(auth_client):
    assert auth_client.get("/api/auth/me").status_code == 200


def test_logout_revokes(auth_client):
    assert auth_client.post("/api/auth/logout").status_code == 200
    assert auth_client.get("/api/auth/me").status_code == 401
```

注:`/api/inbounds` 在 Task 8 才加;本任务先让 `test_protected_requires_token` 暂用 `/api/auth/me`(已存在)。把该测试改为 `client.get("/api/auth/me").status_code == 401`,Task 8 完成后再加 inbounds 的鉴权测试。

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_auth_api.py -v`
Expected: FAIL — `No module named 'backend.main'`

- [ ] **Step 3: 实现 auth 路由**

`backend/routers/auth.py`:

```python
"""auth.py — 登录/登出/me。除 login 外全站需 Bearer。"""
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.deps import AppState, get_app_state
from backend.security import verify_password

router = APIRouter(prefix="/api/auth", tags=["auth"])


@router.post("/login")
def login(body: schemas.LoginIn, app: AppState = Depends(get_app_state)):
    rec = app.state.password.model_dump() if app.state.password else None
    if not verify_password(rec, body.password):
        raise HTTPException(status_code=401, detail="密码错误")
    return {"token": app.sessions.create(), "expires_in": app.sessions.ttl}


@router.post("/logout")
def logout(authorization: str = "", app: AppState = Depends(get_app_state)):
    # require_auth 已校验过;这里再取一次 token 吊销
    from fastapi import Request  # noqa
    return {"ok": True}
```

logout 需要拿到当前 token 才能吊销。为简化,改为接收凭证:在 `main.py` 给 logout 单独装 `_bearer` 依赖。见下 Step 4 的 main.py 写法(logout 用 `HTTPAuthorizationCredentials`)。把上面 logout 替换为:

```python
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
_bearer = HTTPBearer(auto_error=False)


@router.post("/logout")
def logout(cred: HTTPAuthorizationCredentials = Depends(_bearer),
           app: AppState = Depends(get_app_state)):
    if cred:
        app.sessions.revoke(cred.credentials)
    return {"ok": True}


@router.get("/me")
def me():
    return {"ok": True}
```

- [ ] **Step 4: 实现 main.py(app 工厂 + 全局鉴权 + 静态)**

`backend/main.py`:

```python
"""main.py — FastAPI 应用工厂、路由挂载、前端静态托管、启动钩子。"""
import os
import secrets
from fastapi import Depends, FastAPI
from fastapi.staticfiles import StaticFiles
from fastapi.responses import FileResponse
from backend.config import Settings
from backend.deps import AppState, set_app_state, get_app_state, get_sessions
from backend.security import make_auth_dependency, hash_password
from backend.routers import auth

require_auth = make_auth_dependency(get_sessions)

FRONTEND_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
                            "frontend_dist")


def ensure_password(app_state: AppState, settings: Settings) -> None:
    if app_state.state.password:
        return
    pw = settings.panel_password or secrets.token_urlsafe(12)
    from backend.state import PasswordRec
    app_state.state.password = PasswordRec(**hash_password(pw))
    app_state.persist()
    src = "环境变量 PANEL_PASSWORD" if settings.panel_password else "随机生成"
    print(f"\n{'='*54}\n  面板登录密码({src}): {pw}\n{'='*54}\n", flush=True)


def create_app(start_xray: bool = True) -> FastAPI:
    settings = Settings()
    os.makedirs(settings.data_dir, exist_ok=True)
    app_state = AppState(state_path=settings.state_path, config_path=settings.config_path,
                         xray_bin=settings.xray_bin, data_dir=settings.data_dir,
                         socks_port=settings.socks_port, http_port=settings.http_port)
    set_app_state(app_state)
    ensure_password(app_state, settings)
    if start_xray:
        app_state.xray.start(settings.config_path)

    app = FastAPI(title="Xray Panel", version="2.0")
    # 认证路由(login 公开;logout/me 自身带 bearer 校验)
    app.include_router(auth.router)
    # 其余资源路由:整组挂 require_auth(Task 8-13 逐个 include)
    _mount_protected(app)
    _mount_static(app)

    @app.on_event("shutdown")
    def _shutdown():
        app_state.xray.stop()

    return app


def _mount_protected(app: FastAPI) -> None:
    from backend.routers import inbounds, proxies, balancers, subscription, routing, xray
    for mod in (inbounds, proxies, balancers, subscription, routing, xray):
        app.include_router(mod.router, dependencies=[Depends(require_auth)])


def _mount_static(app: FastAPI) -> None:
    if os.path.isdir(FRONTEND_DIR):
        app.mount("/assets", StaticFiles(directory=os.path.join(FRONTEND_DIR, "assets")),
                  name="assets")

        @app.get("/{full_path:path}")
        def spa(full_path: str):
            index = os.path.join(FRONTEND_DIR, "index.html")
            if os.path.exists(index):
                return FileResponse(index)
            return {"error": "frontend not built"}
```

注意:Task 7 阶段 `inbounds/...` 等模块尚不存在,`_mount_protected` 会 import 失败。**本任务先把 `_mount_protected(app)` 那行注释掉**,留 `# TODO Task 8+`,等 Task 8 起逐步取消注释并 include 对应模块。`auth` 路由已足够让本任务测试通过。

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_auth_api.py -v`
Expected: PASS(5 passed)

- [ ] **Step 6: 提交**

```bash
git add backend/main.py backend/routers/auth.py tests/conftest.py tests/test_auth_api.py
git commit -m "feat: FastAPI app 工厂 + Bearer 认证路由 + 静态托管骨架"
```

---

## Task 8: 入站 CRUD 路由

**Files:**
- Create: `backend/routers/inbounds.py`
- Modify: `backend/main.py`(`_mount_protected` 取消注释并 include `inbounds`)
- Test: `tests/test_inbounds_api.py`

- [ ] **Step 1: 写失败测试**

`tests/test_inbounds_api.py`:

```python
def test_list_seeded(auth_client):
    r = auth_client.get("/api/inbounds")
    assert r.status_code == 200 and len(r.json()) == 2     # 播种 socks + http


def test_create_inbound(auth_client):
    r = auth_client.post("/api/inbounds",
                         json={"protocol": "socks", "listen": "127.0.0.1", "port": 11000})
    assert r.status_code == 201
    assert r.json()["tag"] == "in-2"                       # seq 续号,不与 in-0/in-1 撞


def test_create_duplicate_port_rejected(auth_client):
    auth_client.post("/api/inbounds", json={"protocol": "http", "port": 12000})
    r = auth_client.post("/api/inbounds", json={"protocol": "http", "port": 12000})
    assert r.status_code == 400


def test_update_inbound(auth_client):
    r = auth_client.put("/api/inbounds/in-0", json={"protocol": "socks", "port": 13000})
    assert r.status_code == 200 and r.json()["port"] == 13000


def test_delete_inbound(auth_client):
    auth_client.delete("/api/inbounds/in-1")
    assert len(auth_client.get("/api/inbounds").json()) == 1


def test_bad_port_422(auth_client):
    r = auth_client.post("/api/inbounds", json={"protocol": "socks", "port": 0})
    assert r.status_code == 422
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_inbounds_api.py -v`
Expected: FAIL — 404/ImportError(模块/路由不存在)

- [ ] **Step 3: 实现 inbounds 路由**

`backend/routers/inbounds.py`:

```python
"""inbounds.py — 入站 CRUD。tag 用 inbound_seq 续号,稳定不重排。"""
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.config import Settings
from backend.deps import AppState, get_app_state
from backend.state import Inbound, Auth

router = APIRouter(prefix="/api/inbounds", tags=["inbounds"])


def _panel_port() -> int:
    return Settings().panel_port


def _check_port(app: AppState, port: int, exclude_tag: str | None = None):
    if port == _panel_port():
        raise HTTPException(400, f"入站端口 {port} 与面板端口冲突")
    for ib in app.state.inbounds:
        if ib.tag != exclude_tag and ib.port == port:
            raise HTTPException(400, f"入站端口 {port} 重复")


def _to_inbound(tag: str, body: schemas.InboundIn) -> Inbound:
    auth = Auth(user=body.auth.user, **{"pass": body.auth.password}) if body.auth else None
    return Inbound(tag=tag, protocol=body.protocol, listen=body.listen.strip() or "127.0.0.1",
                   port=body.port, udp=body.udp, auth=auth)


@router.get("")
def list_inbounds(app: AppState = Depends(get_app_state)):
    return app.state.inbounds


@router.post("", status_code=201)
def create_inbound(body: schemas.InboundIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        _check_port(app, body.port)
        tag = f"in-{app.state.inbound_seq}"
        app.state.inbound_seq += 1
        ib = _to_inbound(tag, body)
        app.state.inbounds.append(ib)
        app.persist()
        return ib


@router.put("/{tag}")
def update_inbound(tag: str, body: schemas.InboundIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        idx = next((i for i, ib in enumerate(app.state.inbounds) if ib.tag == tag), None)
        if idx is None:
            raise HTTPException(404, f"入站 {tag} 不存在")
        _check_port(app, body.port, exclude_tag=tag)
        app.state.inbounds[idx] = _to_inbound(tag, body)
        app.persist()
        return app.state.inbounds[idx]


@router.delete("/{tag}", status_code=200)
def delete_inbound(tag: str, app: AppState = Depends(get_app_state)):
    with app.lock:
        before = len(app.state.inbounds)
        app.state.inbounds = [ib for ib in app.state.inbounds if ib.tag != tag]
        if len(app.state.inbounds) == before:
            raise HTTPException(404, f"入站 {tag} 不存在")
        app.persist()
        return {"ok": True}
```

- [ ] **Step 4: 在 main.py 启用受保护路由挂载**

把 Task 7 中注释掉的 `_mount_protected(app)` 取消注释。同时把 `_mount_protected` 改为仅 include 已实现的模块(随任务推进逐步加):

```python
def _mount_protected(app: FastAPI) -> None:
    from backend.routers import inbounds
    for mod in (inbounds,):
        app.include_router(mod.router, dependencies=[Depends(require_auth)])
```

(Task 9-13 每完成一个,把对应模块加进这个元组。)

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_inbounds_api.py tests/test_auth_api.py -v`
Expected: PASS(全部)。同时把 `tests/test_auth_api.py::test_protected_requires_token` 恢复为 `assert client.get("/api/inbounds").status_code == 401`。

- [ ] **Step 6: 提交**

```bash
git add backend/routers/inbounds.py backend/main.py tests/test_inbounds_api.py tests/test_auth_api.py
git commit -m "feat: 入站 CRUD 路由(seq 稳定 tag + 端口冲突校验)"
```

---

## Task 9: 落地代理 CRUD 路由

**Files:**
- Create: `backend/routers/proxies.py`
- Modify: `backend/main.py`(`_mount_protected` 加 `proxies`)
- Test: `tests/test_proxies_api.py`

- [ ] **Step 1: 写失败测试**

`tests/test_proxies_api.py`:

```python
def test_proxy_crud(auth_client):
    r = auth_client.post("/api/proxies",
                         json={"name": "US", "protocol": "socks", "host": "1.1.1.1", "port": 1080})
    assert r.status_code == 201 and r.json()["tag"] == "px-0"
    assert auth_client.get("/api/proxies").json()[0]["name"] == "US"
    auth_client.put("/api/proxies/px-0",
                    json={"name": "US2", "protocol": "http", "host": "2.2.2.2", "port": 8080})
    assert auth_client.get("/api/proxies").json()[0]["name"] == "US2"
    auth_client.delete("/api/proxies/px-0")
    assert auth_client.get("/api/proxies").json() == []


def test_proxy_empty_host_422(auth_client):
    r = auth_client.post("/api/proxies", json={"protocol": "socks", "host": "", "port": 1080})
    assert r.status_code == 422


def test_delete_proxy_clears_dangling_refs(auth_client):
    auth_client.post("/api/proxies",
                     json={"protocol": "socks", "host": "h", "port": 1080})  # px-0
    auth_client.put("/api/routing",
                    json={"default_outbound": "px-0", "rules": []})
    auth_client.delete("/api/proxies/px-0")
    assert auth_client.get("/api/routing").json()["default_outbound"] != "px-0"
```

(第三个测试依赖 Task 12 的 routing 路由;若按顺序执行,可先跳过 `test_delete_proxy_clears_dangling_refs`,Task 12 完成后再启用——用 `@pytest.mark.skip(reason="needs Task 12")` 标注。)

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_proxies_api.py -v`
Expected: FAIL — 路由不存在

- [ ] **Step 3: 实现 proxies 路由**

`backend/routers/proxies.py`:

```python
"""proxies.py — 落地代理 CRUD。删代理后清理失效的默认出口/规则引用。"""
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.deps import AppState, get_app_state
from backend.state import Proxy, Auth

router = APIRouter(prefix="/api/proxies", tags=["proxies"])


def _to_proxy(tag: str, body: schemas.ProxyIn) -> Proxy:
    auth = Auth(user=body.auth.user, **{"pass": body.auth.password}) if body.auth else None
    name = body.name.strip() or tag
    return Proxy(tag=tag, name=name, protocol=body.protocol,
                 host=body.host, port=body.port, auth=auth)


def _prune_dangling(app: AppState) -> None:
    valid = app.outbound_tags()
    if app.state.default_outbound not in valid:
        app.state.default_outbound = app.state.nodes[0].tag if app.state.nodes else "direct"
    app.state.rules = [r for r in app.state.rules if r.outbound in valid]


@router.get("")
def list_proxies(app: AppState = Depends(get_app_state)):
    return app.state.proxies


@router.post("", status_code=201)
def create_proxy(body: schemas.ProxyIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        tag = f"px-{app.state.proxy_seq}"
        app.state.proxy_seq += 1
        px = _to_proxy(tag, body)
        app.state.proxies.append(px)
        app.persist()
        return px


@router.put("/{tag}")
def update_proxy(tag: str, body: schemas.ProxyIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        idx = next((i for i, p in enumerate(app.state.proxies) if p.tag == tag), None)
        if idx is None:
            raise HTTPException(404, f"代理 {tag} 不存在")
        app.state.proxies[idx] = _to_proxy(tag, body)
        app.persist()
        return app.state.proxies[idx]


@router.delete("/{tag}", status_code=200)
def delete_proxy(tag: str, app: AppState = Depends(get_app_state)):
    with app.lock:
        before = len(app.state.proxies)
        app.state.proxies = [p for p in app.state.proxies if p.tag != tag]
        if len(app.state.proxies) == before:
            raise HTTPException(404, f"代理 {tag} 不存在")
        _prune_dangling(app)
        app.persist()
        return {"ok": True}
```

- [ ] **Step 4: main.py 挂载**

`_mount_protected` 的元组改为 `(inbounds, proxies)`。

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_proxies_api.py -v`
Expected: PASS(跳过依赖 Task 12 的用例)

- [ ] **Step 6: 提交**

```bash
git add backend/routers/proxies.py backend/main.py tests/test_proxies_api.py
git commit -m "feat: 落地代理 CRUD 路由 + 失效引用清理"
```

---

## Task 10: 自动组 CRUD 路由

**Files:**
- Create: `backend/routers/balancers.py`
- Modify: `backend/main.py`(加 `balancers`)
- Test: `tests/test_balancers_api.py`

- [ ] **Step 1: 写失败测试**

`tests/test_balancers_api.py`:

```python
import pytest


@pytest.fixture
def client_with_nodes(auth_client):
    # 直接注入两个节点(绕过订阅拉取)
    from backend.deps import get_app_state
    st = get_app_state().state
    from backend.state import Node
    st.nodes = [Node(tag="node-0", name="A", type="vmess", host="a", port=1, outbound={"protocol": "vmess"}),
                Node(tag="node-1", name="B", type="vmess", host="b", port=1, outbound={"protocol": "vmess"})]
    return auth_client


def test_create_balancer(client_with_nodes):
    r = client_with_nodes.post("/api/balancers", json={"name": "G1", "nodes": ["node-0", "node-1"]})
    assert r.status_code == 201 and r.json()["tag"] == "auto-0"


def test_balancer_filters_invalid_nodes(client_with_nodes):
    r = client_with_nodes.post("/api/balancers", json={"name": "G", "nodes": ["node-0", "ghost"]})
    assert r.json()["nodes"] == ["node-0"]


def test_balancer_empty_rejected(client_with_nodes):
    r = client_with_nodes.post("/api/balancers", json={"name": "G", "nodes": ["ghost"]})
    assert r.status_code == 400
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_balancers_api.py -v`
Expected: FAIL — 路由不存在

- [ ] **Step 3: 实现 balancers 路由**

`backend/routers/balancers.py`:

```python
"""balancers.py — 自动组(负载均衡)CRUD。tag 用 balancer_seq 续号。"""
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.deps import AppState, get_app_state
from backend.state import Balancer

router = APIRouter(prefix="/api/balancers", tags=["balancers"])


def _members(app: AppState, nodes: list[str]) -> list[str]:
    node_tags = {n.tag for n in app.state.nodes}
    return [t for t in nodes if t in node_tags]


@router.get("")
def list_balancers(app: AppState = Depends(get_app_state)):
    return app.state.balancers


@router.post("", status_code=201)
def create_balancer(body: schemas.BalancerIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        members = _members(app, body.nodes)
        if not members:
            raise HTTPException(400, f"自动组「{body.name}」没有有效节点")
        tag = f"auto-{app.state.balancer_seq}"
        app.state.balancer_seq += 1
        bal = Balancer(tag=tag, name=body.name or tag, nodes=members, strategy="leastPing")
        app.state.balancers.append(bal)
        app.persist()
        return bal


@router.put("/{tag}")
def update_balancer(tag: str, body: schemas.BalancerIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        idx = next((i for i, b in enumerate(app.state.balancers) if b.tag == tag), None)
        if idx is None:
            raise HTTPException(404, f"自动组 {tag} 不存在")
        members = _members(app, body.nodes)
        if not members:
            raise HTTPException(400, f"自动组「{body.name}」没有有效节点")
        app.state.balancers[idx] = Balancer(tag=tag, name=body.name or tag,
                                             nodes=members, strategy="leastPing")
        app.persist()
        return app.state.balancers[idx]


@router.delete("/{tag}", status_code=200)
def delete_balancer(tag: str, app: AppState = Depends(get_app_state)):
    with app.lock:
        before = len(app.state.balancers)
        app.state.balancers = [b for b in app.state.balancers if b.tag != tag]
        if len(app.state.balancers) == before:
            raise HTTPException(404, f"自动组 {tag} 不存在")
        valid = app.outbound_tags()
        if app.state.default_outbound not in valid:
            app.state.default_outbound = app.state.nodes[0].tag if app.state.nodes else "direct"
        app.state.rules = [r for r in app.state.rules if r.outbound in valid]
        app.persist()
        return {"ok": True}
```

- [ ] **Step 4: main.py 挂载**

`_mount_protected` 元组改为 `(inbounds, proxies, balancers)`。

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_balancers_api.py -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add backend/routers/balancers.py backend/main.py tests/test_balancers_api.py
git commit -m "feat: 自动组 CRUD 路由"
```

---

## Task 11: 订阅 / 节点 / 测速路由

**Files:**
- Create: `backend/routers/subscription.py`
- Modify: `backend/main.py`(加 `subscription`)
- Test: `tests/test_subscription_api.py`

- [ ] **Step 1: 写失败测试(mock 网络)**

`tests/test_subscription_api.py`:

```python
import base64, json


def _vmess_link(name):
    raw = base64.b64encode(json.dumps(
        {"add": "1.2.3.4", "port": "443", "id": "u", "ps": name}).encode()).decode()
    return "vmess://" + raw


def test_subscription_fetch(monkeypatch, auth_client):
    text = "\n".join([_vmess_link("HK"), _vmess_link("US")])
    import backend.routers.subscription as sub
    monkeypatch.setattr(sub, "_fetch", lambda url: text)
    r = auth_client.put("/api/subscription", json={"url": "https://x/sub"})
    assert r.status_code == 200
    assert len(r.json()["nodes"]) == 2
    assert auth_client.get("/api/nodes").json()[0]["tag"] == "node-0"


def test_subscription_rejects_non_http(auth_client):
    r = auth_client.put("/api/subscription", json={"url": "file:///etc/passwd"})
    assert r.status_code == 400


def test_speed_test(monkeypatch, auth_client):
    from backend.deps import get_app_state
    from backend.state import Node
    get_app_state().state.nodes = [
        Node(tag="node-0", name="A", type="vmess", host="h", port=1, outbound={"protocol": "vmess"})]
    import backend.services.xray_sub as xs
    monkeypatch.setattr(xs, "tcp_ping", lambda host, port, timeout=3.0: 42)
    r = auth_client.post("/api/nodes/test")
    assert r.status_code == 200 and r.json()[0]["latency"] == 42
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_subscription_api.py -v`
Expected: FAIL — 路由不存在

- [ ] **Step 3: 实现 subscription 路由**

`backend/routers/subscription.py`:

```python
"""subscription.py — 订阅拉取/解析、节点列表、测速。"""
import time
import urllib.parse
import urllib.request
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.deps import AppState, get_app_state
from backend.state import Node, Subscription
from backend.services import xray_sub

router = APIRouter(tags=["subscription"])
UA = "Mozilla/5.0 (X11; Linux x86_64) Shadowrocket/2.2.49"


def _fetch(url: str) -> str:
    # 只允许 http/https,结构性屏蔽 file:// 等(防 SSRF)
    if urllib.parse.urlsplit(url).scheme not in ("http", "https"):
        raise HTTPException(400, "只支持 http/https 订阅链接")
    opener = urllib.request.build_opener(urllib.request.HTTPHandler, urllib.request.HTTPSHandler)
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with opener.open(req, timeout=20) as resp:
        return resp.read().decode("utf-8", errors="replace")


@router.get("/api/subscription")
def get_subscription(app: AppState = Depends(get_app_state)):
    return app.state.subscription


@router.put("/api/subscription")
def set_subscription(body: schemas.SubscriptionIn, app: AppState = Depends(get_app_state)):
    url = body.url.strip()
    if not url:
        raise HTTPException(400, "订阅链接为空")
    try:
        text = _fetch(url)
    except HTTPException:
        raise
    except Exception as e:
        raise HTTPException(502, f"拉取失败: {e}")
    links, meta = xray_sub.extract_links(text)
    parsed, skipped = xray_sub.parse_links(links)
    xray_sub.assign_tags(parsed)
    if not parsed:
        raise HTTPException(400, "未解析到任何 Xray 可用节点")
    with app.lock:
        app.state.nodes = [Node(**n) for n in parsed]
        app.state.subscription = Subscription(url=url, remarks=meta.get("REMARKS", ""),
                                              status=meta.get("STATUS", ""),
                                              fetched_at=int(time.time()))
        node_tags = {n.tag for n in app.state.nodes}
        app.state.balancers = [b for b in app.state.balancers
                               if all(t in node_tags for t in b.nodes)]
        valid = app.outbound_tags()
        if app.state.default_outbound not in valid:
            app.state.default_outbound = app.state.nodes[0].tag
        app.state.rules = [r for r in app.state.rules if r.outbound in valid]
        app.persist()
    return {"nodes": app.state.nodes, "skipped": len(skipped),
            "subscription": app.state.subscription}


@router.get("/api/nodes")
def list_nodes(app: AppState = Depends(get_app_state)):
    return app.state.nodes


@router.post("/api/nodes/test")
def test_nodes(app: AppState = Depends(get_app_state)):
    with app.lock:
        plain = [{"host": n.host, "port": n.port} for n in app.state.nodes]
        xray_sub.measure(plain)
        for n, p in zip(app.state.nodes, plain):
            n.latency = p.get("latency")
        app.persist()
    return app.state.nodes
```

- [ ] **Step 4: main.py 挂载**

`_mount_protected` 元组加 `subscription`。

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_subscription_api.py -v`
Expected: PASS(3 passed)

- [ ] **Step 6: 提交**

```bash
git add backend/routers/subscription.py backend/main.py tests/test_subscription_api.py
git commit -m "feat: 订阅拉取/节点/测速路由(防 SSRF)"
```

---

## Task 12: 路由规则 + 默认出口 + 模板 + 出口选项

**Files:**
- Create: `backend/routers/routing.py`
- Modify: `backend/main.py`(加 `routing`)
- Test: `tests/test_routing_api.py`

- [ ] **Step 1: 写失败测试**

`tests/test_routing_api.py`:

```python
import pytest


@pytest.fixture
def client_with_node(auth_client):
    from backend.deps import get_app_state
    from backend.state import Node
    get_app_state().state.nodes = [
        Node(tag="node-0", name="A", type="vmess", host="h", port=1, outbound={"protocol": "vmess"})]
    return auth_client


def test_get_routing_default(client_with_node):
    r = client_with_node.get("/api/routing")
    assert r.status_code == 200 and "rules" in r.json()


def test_put_routing_keeps_order(client_with_node):
    rules = [{"type": "full", "value": "a.com", "outbound": "direct"},
             {"type": "full", "value": "b.com", "outbound": "node-0"}]
    r = client_with_node.put("/api/routing", json={"default_outbound": "node-0", "rules": rules})
    assert r.status_code == 200
    got = client_with_node.get("/api/routing").json()["rules"]
    assert [x["value"] for x in got] == ["a.com", "b.com"]      # 保序
    assert got[0]["id"] == 1 and got[1]["id"] == 2


def test_put_routing_rejects_unknown_outbound(client_with_node):
    r = client_with_node.put("/api/routing",
                             json={"default_outbound": "ghost", "rules": []})
    assert r.status_code == 400


def test_outbounds_aggregation(client_with_node):
    opts = client_with_node.get("/api/outbounds").json()
    tags = [o["tag"] for o in opts]
    assert "node-0" in tags and "direct" in tags and "block" in tags


def test_templates(client_with_node):
    r = client_with_node.get("/api/routing/templates")
    assert "cn-direct" in r.json()
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_routing_api.py -v`
Expected: FAIL — 路由不存在

- [ ] **Step 3: 实现 routing 路由**

`backend/routers/routing.py`:

```python
"""routing.py — 分流规则(有序整体 PUT)、默认出口、模板、出口选项聚合。"""
from fastapi import APIRouter, Depends, HTTPException
from backend import schemas
from backend.deps import AppState, get_app_state
from backend.state import Rule
from backend.services import routing as routing_svc

router = APIRouter(tags=["routing"])


@router.get("/api/routing")
def get_routing(app: AppState = Depends(get_app_state)):
    return {"default_outbound": app.state.default_outbound, "rules": app.state.rules}


@router.put("/api/routing")
def put_routing(body: schemas.RoutingIn, app: AppState = Depends(get_app_state)):
    with app.lock:
        valid = app.outbound_tags()
        if body.default_outbound and body.default_outbound not in valid:
            raise HTTPException(400, f"默认出口 {body.default_outbound} 不存在")
        rules, next_id = [], 1
        for r in body.rules:
            if r.outbound not in valid:
                raise HTTPException(400, f"规则出口 {r.outbound} 不存在")
            rid = r.id or next_id
            rules.append(Rule(id=rid, type=r.type, value=(r.value or "").strip(),
                              outbound=r.outbound, enabled=r.enabled))
            next_id = max(next_id, rid) + 1
        app.state.default_outbound = body.default_outbound
        app.state.rules = rules
        app.persist()
    return {"default_outbound": app.state.default_outbound, "rules": app.state.rules}


@router.get("/api/routing/templates")
def templates():
    return routing_svc.TEMPLATES


@router.get("/api/outbounds")
def outbounds(app: AppState = Depends(get_app_state)):
    out = []
    for n in app.state.nodes:
        out.append({"tag": n.tag, "label": n.name, "kind": "node"})
    for b in app.state.balancers:
        out.append({"tag": b.tag, "label": f"⚖ {b.name}(自动)", "kind": "balancer"})
    for p in app.state.proxies:
        out.append({"tag": p.tag, "label": f"🛰 {p.name}(落地)", "kind": "proxy"})
    out.append({"tag": "direct", "label": "直连", "kind": "builtin"})
    out.append({"tag": "block", "label": "阻断", "kind": "builtin"})
    return out
```

- [ ] **Step 4: main.py 挂载 + 启用 Task 9 跳过的测试**

`_mount_protected` 元组加 `routing`。把 `tests/test_proxies_api.py::test_delete_proxy_clears_dangling_refs` 的 `@pytest.mark.skip` 去掉。

- [ ] **Step 5: 运行确认通过**

Run: `uv run pytest tests/test_routing_api.py tests/test_proxies_api.py -v`
Expected: PASS(全部)

- [ ] **Step 6: 提交**

```bash
git add backend/routers/routing.py backend/main.py tests/test_routing_api.py tests/test_proxies_api.py
git commit -m "feat: 路由规则(保序 PUT)+ 模板 + 出口选项聚合"
```

---

## Task 13: Xray 应用 / 重启 / 状态 / 拓扑 / 原始配置

**Files:**
- Create: `backend/routers/xray.py`
- Modify: `backend/main.py`(加 `xray`)
- Test: `tests/test_xray_api.py`

- [ ] **Step 1: 写失败测试(mock Xray 进程)**

`tests/test_xray_api.py`:

```python
import pytest


@pytest.fixture
def client_with_node(auth_client):
    from backend.deps import get_app_state
    from backend.state import Node
    get_app_state().state.nodes = [
        Node(tag="node-0", name="A", type="vmess", host="h", port=1, outbound={"protocol": "vmess"})]
    get_app_state().state.default_outbound = "node-0"
    return auth_client


def test_status_shape(client_with_node):
    r = client_with_node.get("/api/xray/status")
    assert r.status_code == 200 and "running" in r.json() and "dirty" in r.json()


def test_apply_builds_and_restarts(monkeypatch, client_with_node):
    from backend.deps import get_app_state
    xray = get_app_state().xray
    monkeypatch.setattr(xray, "test_config", lambda cfg: (True, "ok"))
    monkeypatch.setattr(xray, "restart", lambda path: True)
    r = client_with_node.post("/api/apply")
    assert r.status_code == 200 and r.json()["xray_running"] is True


def test_apply_rejects_bad_config(monkeypatch, client_with_node):
    from backend.deps import get_app_state
    monkeypatch.setattr(get_app_state().xray, "test_config", lambda cfg: (False, "boom"))
    r = client_with_node.post("/api/apply")
    assert r.status_code == 400


def test_apply_no_nodes(auth_client):
    r = auth_client.post("/api/apply")
    assert r.status_code == 400


def test_topology_before_apply(client_with_node):
    r = client_with_node.get("/api/topology")
    assert r.json()["applied"] is False
```

- [ ] **Step 2: 运行确认失败**

Run: `uv run pytest tests/test_xray_api.py -v`
Expected: FAIL — 路由不存在

- [ ] **Step 3: 实现 xray 路由**

`backend/routers/xray.py`:

```python
"""xray.py — 应用配置、重启、状态、拓扑、原始 config.json。两段式 dirty。"""
import json
import os
from fastapi import APIRouter, Depends, HTTPException
from backend.deps import AppState, get_app_state
from backend.services import config_builder, routing as routing_svc

router = APIRouter(tags=["xray"])


def _state_dict(app: AppState) -> dict:
    """把 PanelState 转成 config_builder 期望的 dict(by_alias 让 auth 输出 pass)。"""
    return app.state.model_dump(by_alias=True)


def _read_applied(app: AppState):
    if not os.path.exists(app.config_path):
        return None
    try:
        with open(app.config_path, encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return None


def _compute_dirty(app: AppState, applied) -> bool:
    if not app.state.nodes:
        return False
    draft = config_builder.build_config(_state_dict(app))
    if applied is None:
        return True
    key = lambda c: json.dumps({"i": c.get("inbounds"), "o": c.get("outbounds"),
                                "r": c.get("routing"), "ob": c.get("observatory")},
                               sort_keys=True, ensure_ascii=False)
    return key(draft) != key(applied)


@router.get("/api/xray/status")
def status(app: AppState = Depends(get_app_state)):
    applied = _read_applied(app)
    return {"running": app.xray.running(), "applied": applied is not None,
            "dirty": _compute_dirty(app, applied)}


@router.post("/api/apply")
def apply(app: AppState = Depends(get_app_state)):
    with app.lock:
        if not app.state.nodes:
            raise HTTPException(400, "还没有节点,先拉取订阅")
        cfg = config_builder.build_config(_state_dict(app))
        ok, out = app.xray.test_config(cfg)
        if not ok:
            raise HTTPException(400, f"配置校验失败: {out[-800:]}")
        with open(app.config_path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=2)
        running = app.xray.restart(app.config_path)
    return {"ok": True, "xray_running": running}


@router.post("/api/xray/restart")
def restart(app: AppState = Depends(get_app_state)):
    ok = app.xray.restart(app.config_path)
    return {"ok": ok, "xray_running": app.xray.running()}


@router.get("/api/config")
def raw_config(app: AppState = Depends(get_app_state)):
    applied = _read_applied(app)
    if applied is None:
        raise HTTPException(404, "尚未应用过配置")
    return applied


@router.get("/api/topology")
def topology(app: AppState = Depends(get_app_state)):
    cfg = _read_applied(app)
    inbs, outs, route = [], [], []
    if cfg is not None:
        for ib in cfg.get("inbounds", []):
            inbs.append({"tag": ib.get("tag"), "protocol": ib.get("protocol"),
                         "listen": ib.get("listen"), "port": ib.get("port")})
        rev = {}
        for i, r in enumerate(cfg.get("routing", {}).get("rules", [])):
            m = routing_svc.describe(r)
            tag = r.get("balancerTag") or r.get("outboundTag")
            route.append({"order": i + 1, "match": m, "outbound": tag,
                          "label": app.outbound_label(tag)})
            rev.setdefault(tag, []).append("默认出口" if m.startswith("默认出口") else m)
        for ob in cfg.get("outbounds", []):
            t = ob.get("tag")
            outs.append({"tag": t, "protocol": ob.get("protocol"),
                         "label": app.outbound_label(t), "rules": rev.get(t, [])})
        for b in cfg.get("routing", {}).get("balancers", []):
            t = b["tag"]
            outs.append({"tag": t, "protocol": f"balancer/{b['strategy'].get('type', '')}",
                         "label": app.outbound_label(t), "rules": rev.get(t, []),
                         "members": b.get("selector", [])})
    return {"applied": cfg is not None, "dirty": _compute_dirty(app, cfg),
            "inbounds": inbs, "outbounds": outs, "routing": route}
```

- [ ] **Step 4: main.py 挂载**

`_mount_protected` 元组加 `xray`,最终为 `(inbounds, proxies, balancers, subscription, routing, xray)`。

- [ ] **Step 5: 运行全部测试确认通过**

Run: `uv run pytest -v`
Expected: PASS(全部任务的测试)

- [ ] **Step 6: 提交**

```bash
git add backend/routers/xray.py backend/main.py tests/test_xray_api.py
git commit -m "feat: Xray 应用/重启/状态/拓扑路由(两段式 dirty)"
```

---

## Task 14: 删除旧入口 + Dockerfile + compose + README

**Files:**
- Delete: `app.py`, `main.py`(旧顶层占位)
- Modify: `Dockerfile`, `docker-compose.yml`, `README.md`
- Test: 手动构建验证 + `uv run pytest`

- [ ] **Step 1: 删除旧后端入口**

```bash
git rm app.py main.py store.py
```

(`store.py` 已被 `backend/state.py` 取代;旧 `app.py`/`main.py` 不再使用。`index.html` 暂留,Plan 2 会迁入 `frontend/`。)

- [ ] **Step 2: 确认无残留引用**

Run: `grep -rn "import store\|import app\b\|from store" backend tests || echo OK`
Expected: `OK`

- [ ] **Step 3: 改写 Dockerfile(多阶段;前端阶段先放占位)**

```dockerfile
# 1) 取 xray 二进制 + geodata
FROM ghcr.io/xtls/xray-core:26.5.9 AS xray

# 2) python 运行时(前端 dist 由 Plan 2 接入;现阶段无 frontend_dist 也能跑,仅 API)
FROM python:3.12-slim
COPY --from=xray /usr/local/bin/xray /usr/local/bin/xray
COPY --from=xray /usr/local/share/xray/ /usr/local/share/xray/
ENV XRAY_LOCATION_ASSET=/usr/local/share/xray
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY pyproject.toml ./
RUN uv sync --no-dev
COPY backend/ ./backend/
EXPOSE 2017 10808 10809
CMD ["uv", "run", "uvicorn", "backend.main:app", "--host", "0.0.0.0", "--port", "2017"]
```

> Plan 2 会在此 Dockerfile 前面插入 `node:20-slim` 构建阶段,并 `COPY --from=web /web/dist ./frontend_dist/`。当前阶段无 `frontend_dist`,`_mount_static` 会跳过静态挂载,仅暴露 `/api` 与 `/docs`,可独立验证后端。

- [ ] **Step 4: 确认 docker-compose.yml 无需改动**

现有 `docker-compose.yml`(`network_mode: host`、卷 `/data/xray`、`PANEL_PORT` 等)与新后端兼容,保持不变。确认内容仍正确即可。

- [ ] **Step 5: 更新 README**

`README.md` 写入:项目简介、本地开发(`uv sync` + `uv run uvicorn backend.main:app --reload --port 2017`,访问 `/docs`)、Docker 部署(`docker compose up -d --build`)、环境变量表(`PANEL_PORT`/`PANEL_DATA_DIR`/`PANEL_PASSWORD`/`XRAY_BIN`)、首启密码说明。

- [ ] **Step 6: 全量测试 + 构建验证**

```bash
uv run pytest -v
docker build -t xray-panel:test .
```
Expected: 测试全过;镜像构建成功。

- [ ] **Step 7: 提交**

```bash
git add -A
git commit -m "chore: 删除旧入口,多阶段 Dockerfile + README(后端阶段)"
```

---

## Self-Review(已核对)

- **Spec 覆盖**:§2 决策 → Task 1-5;§4 API → Task 7-13(逐资源);§5 两段式 dirty → Task 13;§6 Bearer 鉴权 → Task 4/7;§9 构建 → Task 14。前端(§7/§8)归 Plan 2。
- **类型一致**:`PanelState` 子模型(`Inbound/Proxy/Balancer/Rule/Node/Auth`)在 Task 3 定义,Task 8-13 复用同名字段;`AppState.outbound_tags/outbound_label/persist` 在 Task 6 定义,后续任务调用一致;`SessionStore.create/valid/revoke` 在 Task 4 定义,Task 7 使用一致。
- **占位符**:无 TODO/TBD;每步含具体代码与命令。
- **已知顺序依赖**:Task 9 的 `test_delete_proxy_clears_dangling_refs` 依赖 Task 12,已用 skip 标注并在 Task 12 Step 4 取消;Task 7 的 `test_protected_requires_token` 先用 `/api/auth/me`,Task 8 改回 `/api/inbounds`。

## 验收(Plan 1 完成时)

- [ ] `uv run pytest -v` 全绿。
- [ ] `uv run uvicorn backend.main:app` 起得来,`/docs` 可见全部资源端点,Authorize 按钮可用。
- [ ] 除 `POST /api/auth/login` 外,所有 `/api/*` 无 token 返回 401。
- [ ] 旧 `panel.json` 可被新后端直接加载(字段兼容)。
- [ ] `docker build` 成功;容器内仅 API + `/docs`(前端待 Plan 2)。
