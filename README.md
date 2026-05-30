# xray-panel

轻量级 Xray-core 管理面板，FastAPI 后端 + Vue 前端（前端见 Plan 2）。

通过 RESTful API 管理 Xray 入站、出站、节点、订阅与路由规则，首次启动自动生成随机密码，支持 Bearer Token 鉴权(不透明随机 token + 内存会话表,除登录外所有 /api/* 需鉴权)。

---

## 本地开发

```bash
# 安装依赖
uv sync

# 启动后端（热重载）
uv run uvicorn --factory backend.main:create_app --reload --port 2017
```

访问 API 文档：`http://127.0.0.1:2017/docs`

> **鉴权说明**：先 `POST /api/auth/login` 取 token，再点页面右上角 **Authorize**，粘贴 `Bearer <token>` 即可调用受保护接口。首次启动密码会在终端日志中打印。

---

## 运行测试

```bash
uv run pytest
```

---

## Docker 部署

```bash
# 构建镜像并后台启动
docker compose up -d --build

# 查看首次启动随机密码
docker compose logs | grep 密码

# 浏览器访问面板 API 文档
http://<host>:2017/docs
```

---

## 环境变量

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `PANEL_PORT` | `2017` | 面板监听端口 |
| `PANEL_DATA_DIR` | `/data/xray` | 数据目录（存放 panel.json / config.json） |
| `PANEL_PASSWORD` | 随机生成 | 登录密码；不设则首次启动随机生成并打印到日志 |
| `XRAY_BIN` | `/usr/local/bin/xray` | Xray 可执行文件路径 |

---

## 数据持久化

状态文件存储于 `$PANEL_DATA_DIR/panel.json`，当前生效的 Xray 配置写入 `$PANEL_DATA_DIR/config.json`。Docker 部署时通过卷挂载 `/data/xray:/data/xray` 保证重启不丢数据。
