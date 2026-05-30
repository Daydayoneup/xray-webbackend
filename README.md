# xray-panel

轻量级 Xray-core 服务器管理面板，Golang 后端 + Vue 前端。

## 这是什么

[Xray-core](https://github.com/XTLS/Xray-core) 是一个网络代理工具，通常部署在 Linux 服务器上。但 Xray 本身没有图形界面，所有配置都要通过手动编辑 JSON 文件完成——入站端口、出站节点、分流规则等，修改起来繁琐且容易出错。

**xray-panel** 为 Xray-core 提供一个 Web 管理界面，让你在浏览器中就能完成日常操作：

- **订阅管理**：粘贴机场/自建订阅链接，自动解析 vmess/vless/trojan/shadowsocks 节点
- **入站配置**：管理本地 SOCKS/HTTP 代理端口，支持账号密码鉴权
- **出站管理**：查看所有节点、创建自动负载均衡组、配置落地代理
- **分流规则**：按域名后缀、IP 地区、端口等条件指定流量走向（直连/代理/阻断）
- **配置应用**：修改后一键校验并重启 Xray，配置有误会拦截并提示

单二进制部署，Docker 镜像仅 ~25MB，内存占用 ~10-20MB。

## 快速部署

```bash
git clone <repo-url> && cd xray-panel
docker compose up -d --build
# 查看首次登录密码
docker compose logs | grep 密码
```

浏览器访问 `http://<服务器IP>:2017`。

## 本地开发

```bash
# 后端（Go 1.22+）
cd go-backend
PANEL_PASSWORD=testpw XRAY_BIN=/bin/true go run ./cmd/server/

# 前端（开发热重载）
cd frontend
npm install && npm run dev
```

## 测试

```bash
cd go-backend && go test ./...
```

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `PANEL_PORT` | `2017` | 面板端口 |
| `PANEL_LISTEN` | `0.0.0.0` | 监听地址 |
| `PANEL_DATA_DIR` | `/data/xray` | 数据目录 |
| `PANEL_PASSWORD` | 随机 | 登录密码（不设则首次随机生成） |
| `XRAY_BIN` | `/usr/local/bin/xray` | Xray 路径 |
| `SOCKS_PORT` | `10808` | 默认 SOCKS 入站端口 |
| `HTTP_PORT` | `10809` | 默认 HTTP 入站端口 |
| `SUBSCRIPTION_ALLOW_INTERNAL` | `false` | 允许拉取内网订阅 |

## 项目结构

```
├── go-backend/          # Golang 后端 (chi + net/http)
│   ├── cmd/server/      # 程序入口
│   └── internal/        # 业务逻辑 (config/model/store/auth/handler/service)
├── frontend/            # Vue 前端
├── Dockerfile           # 多阶段构建 (xray + go build + node build → scratch)
└── docker-compose.yml
```

## 数据持久化

状态文件和 Xray 生效配置存储在 `$PANEL_DATA_DIR` 下，Docker 部署时挂载 `./data:/data/xray` 保证重启不丢数据。
