# Xray 面板重构设计

- **日期**: 2026-05-30
- **状态**: 已批准设计,待编写实现计划
- **目标**: 把当前单文件、单页滚动的 Xray 面板重构为专业的多页面管理面板,让入站(inbound)、路由(router)、出站(outbound)管理更易用。

---

## 1. 背景与现状

当前项目是一个零依赖的 Xray-core 管理面板:

- **后端**:`app.py` 用 Python 标准库 `http.server` 实现,业务逻辑已拆成干净的纯模块(`store`、`routing`、`config_builder`、`inbounds`、`proxies`、`xray_sub`、`xray_proc`)。API 是一批 POST 动作端点(`/api/inbounds`、`/api/proxies`、`/api/balancers`、`/api/config`、`/api/apply` 等)。
- **前端**:单个 480 行 `index.html`,所有功能(订阅、入站、节点、自动组、落地代理、默认出口、规则、拓扑)堆在一个长滚动页里,原生 JS + 全局函数。
- **持久化**:单个 `panel.json` 文件(原子写 `.tmp` + `os.replace`)。
- **鉴权**:单密码 + 内存会话 + HttpOnly Cookie。

现状代码质量良好,但单页 UI 在功能变多后不易用;后端动作式 API 也不够规整。

## 2. 重构范围与技术决策

经讨论确定:

| 维度 | 决策 |
|------|------|
| 前端框架 | **Vue 3(Composition API)+ Element Plus + Vite** 构建,多页面 SPA |
| 导航结构 | **左侧边栏**;「出站」展开二级菜单(订阅节点 / 自动组 / 落地代理) |
| 后端 | **FastAPI 深度重构**,替换 `http.server` |
| API 风格 | **RESTful 资源化**,FastAPI 自动生成 OpenAPI 文档(`/docs`) |
| 持久化 | **保留 JSON 文件**(`panel.json`),用 **Pydantic 模型**建模与校验 |
| 鉴权 | **Bearer Token(HTTP 头)**,去掉 Cookie;除登录外所有 `/api/*` 端点强制鉴权 |
| 复用 | 现有纯逻辑模块作为 **service 层基本原样保留**(仅调整 import) |

**不做的事**:不引入数据库(JSON 足够);不引入反向代理(前后端同源同端口);不引入 JWT(默认用不透明随机 token + 内存会话表,可吊销);后端单进程 uvicorn 同时托管 API 与前端静态产物。

## 3. 目录结构

```
xray-panel/
├── pyproject.toml              # 新增: fastapi, uvicorn[standard], pydantic, pydantic-settings
├── backend/
│   ├── main.py                 # 入口: 创建 FastAPI app、挂载路由、托管前端 dist(StaticFiles + SPA fallback)
│   ├── config.py               # Settings(pydantic-settings): PANEL_PORT/DATA_DIR/XRAY_BIN/PANEL_PASSWORD…
│   ├── state.py                # PanelState(Pydantic) + 原子读写 + 全局锁(取代 store.py)
│   ├── schemas.py              # 请求/响应 Pydantic 模型(InboundIn/ProxyIn/RuleIn/BalancerIn…)
│   ├── security.py             # PBKDF2 密码哈希 + 内存会话表 + require_auth 依赖
│   ├── routers/
│   │   ├── auth.py             # 登录/登出/me
│   │   ├── subscription.py     # 订阅拉取、节点列表、测速
│   │   ├── inbounds.py         # 入站 CRUD
│   │   ├── proxies.py          # 落地代理 CRUD
│   │   ├── balancers.py        # 自动组 CRUD
│   │   ├── routing.py          # 分流规则 + 默认出口 + 模板
│   │   └── xray.py             # 应用/重启/状态/原始 config.json/拓扑/出口选项
│   └── services/               # 现有纯逻辑模块迁入(框架无关)
│       ├── config_builder.py   ├── routing.py    ├── inbounds.py
│       ├── proxies.py          ├── xray_sub.py    └── xray_proc.py
├── frontend/
│   ├── package.json  vite.config.js  index.html
│   └── src/
│       ├── main.js  App.vue
│       ├── router/index.js     # hash 路由
│       ├── stores/panel.js     # Pinia: 出口选项 / dirty 状态 / xray 状态等跨页共享
│       ├── api/                # 按资源封装 + axios 拦截器(注入 Bearer、处理 401)
│       ├── layouts/MainLayout.vue
│       └── views/
│           ├── Login.vue  Dashboard.vue  Inbound.vue  Routing.vue  Settings.vue
│           └── outbound/{Subscription,Balancers,Proxies}.vue
├── Dockerfile                  # 多阶段: node 构建前端 → python(uv) 运行时
└── docker-compose.yml
```

**要点**:`services/` 几乎原样保留(已验证的纯逻辑,不重写);手写 `normalize()` 的校验上移到 Pydantic 模型(API 边界自动校验),`to_xray()` 翻译逻辑留在 service。

## 4. RESTful API

统一前缀 `/api`;除 `POST /api/auth/login` 外全部需 Bearer 鉴权。

| 资源 | 方法 + 路径 | 说明 |
|------|-----------|------|
| 认证 | `POST /api/auth/login` · `POST /api/auth/logout` · `GET /api/auth/me` | 登录返回 token / 登出删会话 / 取当前会话 |
| 订阅 | `GET /api/subscription` · `PUT /api/subscription` | 读订阅信息 / 设 URL 并拉取解析 |
| 节点 | `GET /api/nodes` · `POST /api/nodes/test` | 列出节点 / 触发测速 |
| 入站 | `GET /api/inbounds` · `POST` · `PUT /{id}` · `DELETE /{id}` | 入站 CRUD |
| 落地代理 | `GET /api/proxies` · `POST` · `PUT /{id}` · `DELETE /{id}` | 代理 CRUD |
| 自动组 | `GET /api/balancers` · `POST` · `PUT /{id}` · `DELETE /{id}` | 负载均衡组 CRUD |
| 路由 | `GET /api/routing` · `PUT /api/routing` | 规则(有序)+ 默认出口,整体存 |
| 模板 | `GET /api/routing/templates` | 规则模板列表 |
| 出口选项 | `GET /api/outbounds` | 聚合所有可选出口(节点/自动组/落地/direct/block) |
| Xray | `POST /api/apply` · `POST /api/xray/restart` · `GET /api/xray/status` | 构建+校验+落盘+重启 / 仅重启 / 运行状态 |
| 拓扑 | `GET /api/topology` | 已应用配置视图 + dirty 标记 |
| 原始配置 | `GET /api/config` | 查看生成的 config.json(只读) |

**设计约定**:

- 资源 `{id}` 即稳定 tag(`in-0`、`px-0`、`auto-0`、`node-0`),沿用现有体系。
- **路由整体 PUT** 而非逐条 CRUD——规则是有序列表(先命中先生效),整体替换最稳,匹配前端拖拽排序后一次性保存。
- Pydantic 在边界校验(端口范围、协议枚举、出口引用存在性、私网冲突等);校验失败返回 422(Pydantic)或 400(业务校验)。

## 5. 保存 / 应用 / 未应用(dirty)流程

多页面面板的核心交互模型:**改配置分散在各页面,Xray 只在"应用"时重载**。

**两段式(草稿 → 应用)**:

1. **各页面编辑 = 改草稿**:每个 CRUD/PUT 立即把改动持久化到 `panel.json`(`STATE`),但不碰 Xray。
2. **全局「应用配置」= 真正生效**:`POST /api/apply` 才会 `build_config(STATE)` → `xray -test` 校验 → 写 `config.json` → 重启 Xray。沿用现有 `compute_dirty()`(对比草稿配置 vs 已落盘 config.json)。

**UI**:

- `MainLayout` 顶栏常驻**应用栏**:`dirty=true` 高亮「⚠ 有未应用更改 — [应用并重启 Xray]」;`dirty=false` 显示「✅ 配置已生效」。任意页面可见、一键应用。
- Pinia `panel` store 维护全局 `dirty`,每次写操作后从 `GET /api/xray/status` 或写响应刷新。
- 应用失败(`xray -test` 不过)→ 弹窗显示校验报错详情,**不替换**生效配置。

## 6. 鉴权(Bearer Token)

- `POST /api/auth/login`(密码)→ 返回 `{ token, expires_at }`。token = `secrets.token_hex` 不透明随机串,服务端内存会话表 `{token: 过期时间}` 保存(可吊销、7 天过期)。
- 除登录外所有 `/api/*` 用 FastAPI `HTTPBearer` 依赖鉴权:从 `Authorization: Bearer <token>` 取 token → 校验会话表 → 失效/缺失返回 401。`logout`、`me` 也需 token。
- `HTTPBearer` 让 `/docs` 出现「Authorize」按钮,便于调试。
- 前端:token 存 `localStorage`;axios 请求拦截器注入 `Authorization: Bearer`;响应拦截器遇 401 清 token 跳登录。
- 会话存内存(面板重启需重新登录,可接受)。首启随机/环境变量密码逻辑(`PANEL_PASSWORD`)保留。
- 前端静态资源(SPA 空壳)公开;数据全在受保护的 `/api/*` 后。

## 7. 页面设计

列表用 `el-table`,增删改用 `el-dialog`/抽屉或内联,提示用 `ElMessage`/`ElMessageBox`。

| 页面 | 路由 | 内容 |
|------|------|------|
| 登录 | `#/login` | 密码 → 取 token 存 localStorage;未登录访问任何页自动跳此 |
| 概览 | `#/` | 8 张统计卡片(见 §8)+ 出口清单表 + 路由优先级表(读 `/api/topology`) |
| 入站 | `#/inbound` | `el-table` + 编辑对话框:协议(socks/http)、监听地址、端口、账号/密码(可选)、UDP 开关;**监听 0.0.0.0 且无密码 → 行内黄色告警**;端口冲突/越界双重校验 |
| 出站▸订阅节点 | `#/outbound/subscription` | 订阅链接 + 「拉取并解析」;节点 `el-table`(tag/延迟/协议/名称)+ 筛选 + 「测速」+ 「选最快为默认」;延迟颜色分级 |
| 出站▸自动组 | `#/outbound/balancers` | 自动组卡片;每组名称 + 节点多选(`el-transfer`)+ 策略 leastPing;空组拦截 |
| 出站▸落地代理 | `#/outbound/proxies` | `el-table` + 编辑对话框:名称、协议、host、端口、账号/密码(可选) |
| 路由 | `#/routing` | 默认出口下拉(读 `/api/outbounds`)+ **可拖拽规则表**(序号 + 上移/下移 + 拖拽手柄、启用开关、匹配类型、匹配值、出口下拉、删除)+ 工具条(套用模板/导入导出/加规则)+ 底部实时路由优先级预览 |
| 设置 | `#/settings` | 修改密码;只读查看生成的 `config.json`;订阅信息;登出 |

**规则顺序可调是一等需求**:用户需根据不同网络情况调整出口匹配优先级。规则表支持拖拽 + 上移/下移按钮;因路由整体 PUT,数组顺序即优先级,保存不错位。

**全局壳 `MainLayout.vue`**:左侧 `el-menu`(含「出站」二级子菜单)+ 顶栏(Xray 状态灯 + 重启 + 常驻应用栏)+ `<router-view>`。

**共享层**:`api/*.js`(资源封装 + Bearer/401 拦截器)、`stores/panel.js`(出口选项、dirty、xray 状态跨页共享)。

## 8. Dashboard 统计

仅「配置/健康概览」(零额外后端成本,全部从现有状态算出),**不**做实时流量 / 系统指标。

**顶部 8 张统计卡片**:

1. Xray 状态(运行中/未运行 + 重启按钮)
2. 应用状态(已应用 / ⚠ 未应用更改 + 应用按钮)
3. 节点(总数 / 存活数 / 最快节点 + 延迟)
4. 默认出口(label)
5. 入站(数量 + 端口摘要)
6. 出站合计(节点 + 自动组 + 落地)
7. 分流规则(启用 / 总数)
8. 订阅(备注 / 剩余天数 / 拉取时间)

**底部两张表**(读 `/api/topology`):

- 出口清单:`tag` / 出口(label + protocol)/ 命中它的规则
- 路由优先级:`#` / 匹配条件 / 出口

## 9. 构建、部署、测试

**依赖**:`pyproject.toml` 增 `fastapi`、`uvicorn[standard]`、`pydantic`、`pydantic-settings`,用 `uv` 管理。

**前端工程**:依赖 `vue`、`vue-router`、`pinia`、`element-plus`、`axios`;Vite dev server 通过 proxy 把 `/api` 转发到本地 uvicorn 调试。

**多阶段 Dockerfile**:
1. `ghcr.io/xtls/xray-core:26.5.9` 取 xray 二进制 + geodata(沿用)。
2. `node:20-slim` 构建前端 → `dist`。
3. `python:3.12-slim` 运行时:复制 xray、`uv sync --frozen` 装依赖、复制 `backend/` 与前端 `dist` → `frontend_dist/`;`CMD uv run uvicorn backend.main:app --host 0.0.0.0 --port 2017`。

运行时仍单进程 uvicorn 同时托管 `/api/*` 与静态产物(`StaticFiles` + SPA fallback)。`docker-compose.yml` 的 `network_mode: host`、卷挂载、环境变量基本不变。

**测试(TDD)**:

- 后端:`pytest` + FastAPI `TestClient`。覆盖纯逻辑(`config_builder`/`routing`/`xray_sub` 解析)、鉴权(无 token→401)、校验(非法端口/出口→422/400)、两段式 dirty 计算;`xray_proc` 子进程用 mock。
- 前端:核心逻辑(出口选项聚合、规则收集/顺序)用 Vitest 轻量覆盖;UI 以手动冒烟为主。

**迁移兼容**:旧 `panel.json` 字段不变,`state.py` 的 Pydantic 模型兼容现有文件,老部署升级无需手动迁移数据。

## 10. 验收标准

- [ ] 五个功能域各有独立页面(概览/入站/出站三子页/路由/设置),侧边栏导航可切换。
- [ ] 所有现有功能在新 UI 中可用:订阅拉取、测速、入站 CRUD、自动组、落地代理、规则(含拖拽排序、模板、导入导出)、默认出口、应用并重启 Xray。
- [ ] 规则顺序可通过拖拽 + 上移/下移调整,保存后顺序即生效优先级。
- [ ] Bearer Token 鉴权:登录外所有 `/api/*` 无有效 token 返回 401;`/docs` 可用 Authorize 调试。
- [ ] 两段式 dirty:改动落 panel.json 不影响运行;应用栏正确反映未应用状态;应用失败不替换生效配置。
- [ ] Dashboard 展示 8 张统计卡片 + 出口/路由两张表。
- [ ] `docker compose up` 一键构建运行,行为与现网一致;旧 `panel.json` 直接兼容。
- [ ] 后端纯逻辑与 API 有 pytest 覆盖。
