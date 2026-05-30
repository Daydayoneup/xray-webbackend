# xray-panel

轻量级 Xray-core 管理面板，Golang 后端 + Vue 前端。

单二进制部署，scratch 基础镜像 ~25MB。通过 RESTful API 管理 Xray 入站、出站、节点、订阅与路由规则，首次启动自动生成随机密码，支持 Bearer Token 鉴权。

---

## 本地开发

```bash
# 启动后端（需要 Go 1.22+）
cd go-backend
PANEL_PASSWORD=testpw XRAY_BIN=/bin/true go run ./cmd/server/ --port 2017

# 启动前端（开发模式）
cd frontend
npm run dev
```

访问 `http://127.0.0.1:2017`，首次启动密码为环境变量 `PANEL_PASSWORD` 或随机生成（终端日志中打印）。

---

## 运行测试

```bash
cd go-backend
go test ./...
```

---

## Docker 部署

```bash
# 构建镜像并后台启动
docker compose up -d --build

# 查看首次启动随机密码
docker compose logs | grep 密码

# 浏览器访问面板
http://<host>:2017
```

---

## 环境变量

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `PANEL_PORT` | `2017` | 面板监听端口 |
| `PANEL_LISTEN` | `0.0.0.0` | 监听地址 |
| `PANEL_DATA_DIR` | `/data/xray` | 数据目录（存放 panel.json / config.json） |
| `PANEL_PASSWORD` | 随机生成 | 登录密码 |
| `XRAY_BIN` | `/usr/local/bin/xray` | Xray 可执行文件路径 |
| `SOCKS_PORT` | `10808` | SOCKS 入站默认端口 |
| `HTTP_PORT` | `10809` | HTTP 入站默认端口 |
| `SUBSCRIPTION_ALLOW_INTERNAL` | `false` | 放行内网/保留地址订阅 |

---

## 数据持久化

状态文件存储于 `$PANEL_DATA_DIR/panel.json`，当前生效的 Xray 配置写入 `$PANEL_DATA_DIR/config.json`。Docker 部署时通过卷挂载 `./data:/data/xray` 保证重启不丢数据。
