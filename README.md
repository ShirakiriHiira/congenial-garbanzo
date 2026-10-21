# 服务与订阅到期提醒

Telegram Bot + Mini App：管理 SIM/eSIM、VPS、域名、软件与 AI 订阅等有期限的服务，按用户时区提前提醒到期。

- 后端：Go 1.27.3 + SQLite（单实例、单写者）
- 前端：Svelte 5 + Vite + TypeScript
- 文档：[开发规范](docs/DEVELOPMENT.md)｜[HTTP API](docs/API.md)｜[Mini App 规范](docs/MINI_APP_DEVELOPMENT.md)

## 部署

### Docker Compose（推荐）

```sh
cp .env.example .env
chmod 600 .env
# 编辑 .env，至少填写 TELEGRAM_BOT_TOKEN 与 ALLOWED_TELEGRAM_USER_IDS
docker compose build
docker compose up -d
```

验证：

```sh
curl -fsS http://127.0.0.1:8080/readyz   # 后端就绪
curl -fsS http://127.0.0.1:8081/app/     # Mini App 页面
```

- `app`：distroless 非 root 镜像，SQLite 存在命名卷 `/data/remind.db`。
- `web`：nginx 非特权镜像，同源提供 `/app/` 静态资源，并反代 `/api/v1/`、`/healthz`、`/readyz`。
- 两个服务只监听回环端口（8080 / 8081），公网 HTTPS 由反向代理或隧道终止。
- 需要 Mini App 菜单按钮时设置 `MINIAPP_PUBLIC_URL=https://<host>`（只接受 `https://<host>` 或 `https://<host>/app`），用户在私聊 `/start` 后输入框左侧会出现「打开」。
- 备份：先停止写入，再复制 `remind.db` 及 `-wal`/`-shm`；恢复时整体还原。禁止 `docker compose down -v`、删表、清库。

### 直接运行二进制

```sh
CGO_ENABLED=0 GOTOOLCHAIN=go1.27.3+auto go build -trimpath -o ./bin/remind .
export TELEGRAM_BOT_TOKEN='<bot token>'
export ALLOWED_TELEGRAM_USER_IDS='10001,10002'
./bin/remind
```

健康检查：`curl -fsS http://127.0.0.1:8080/readyz` 或 `./bin/remind healthcheck`（只读 `HTTP_ADDR`，不加载 Token、不打开数据库）。

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | 是 | — | Bot Token；本地二进制可由当前目录 `.env` 提供 |
| `ALLOWED_TELEGRAM_USER_IDS` | 是 | — | 逗号分隔的正整数 Telegram 用户 ID；为空则拒绝启动 |
| `DATABASE_PATH` | 否 | `./data/remind.db` | 持久 SQLite 文件；拒绝内存库与 `file:` URI |
| `APP_TIMEZONE` | 否 | `Asia/Taipei` | 新用户默认 IANA 时区 |
| `REMINDER_SCAN_INTERVAL` | 否 | `30s` | 提醒扫描间隔，最小 `1s` |
| `REMINDER_CATCHUP_WINDOW` | 否 | `24h` | 补发窗口 |
| `WEBAPP_AUTH_MAX_AGE` | 否 | `24h` | Mini App 会话默认有效期；用户可在设置页改为 1h/6h/24h/7d |
| `HTTP_ADDR` | 否 | `127.0.0.1:8080` | HTTP 监听地址 |
| `LOG_LEVEL` | 否 | `info` | `debug`/`info`/`warn`/`error` |
| `TELEGRAM_API_PROXY` | 否 | 空 | Telegram API 代理（`http`/`https`/`socks5`/`socks5h`）；留空沿用 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` |
| `MINIAPP_PUBLIC_URL` | 否 | 空 | Mini App 公网地址，只接受 `https://<host>` 或 `https://<host>/app` |
| `MINIAPP_HTTP_PORT` | 否 | `8081` | 仅 Compose：`web` 容器的回环端口 |
| `REMIND_DATA_VOLUME` | 否 | `sim-reminder-data` | 仅 Compose：命名卷名 |

规则：

- 本地二进制只从当前目录 `.env` 读取 `TELEGRAM_BOT_TOKEN`（`.env` 非空时优先于进程环境），其余变量只读进程环境；Compose 读取整个 `.env`。
- 生产环境用环境变量注入配置，不挂载 `.env`。
- 白名单为空即启动失败，没有开放注册。

## 本地开发与调试

依赖：Go 1.27.3、Node 24、npm、curl。

### 跑后端

```sh
cp .env.example .env && chmod 600 .env   # 填入 TELEGRAM_BOT_TOKEN
export ALLOWED_TELEGRAM_USER_IDS='10001'
CGO_ENABLED=0 GOTOOLCHAIN=go1.27.3+auto go build -trimpath -o ./bin/remind .
./bin/remind
```

日志为 JSON（写 stderr），级别由 `LOG_LEVEL` 控制。

### 跑前端

```sh
cd web
npm ci
npm run dev          # http://127.0.0.1:5173/app/，/api 代理到 127.0.0.1:8080
```

### 联调脚本

| 脚本 | 用途 |
| --- | --- |
| `./scripts/test-miniapp.sh` | 隔离调试：假签名 API（8088）+ Vite（5180），不连 Telegram、不读 `.env`、不碰生产库；Ctrl-C 删除临时数据库。打开 http://127.0.0.1:5180/app/ |
| `./scripts/dev-miniapp.sh` | 真实联调：需要真实 Token 与 `ALLOWED_TELEGRAM_USER_IDS`；启动 Go API（8080）、Vite（5173）、HTTPS 隧道（或用 `MINIAPP_PUBLIC_URL`），并为你的私聊安装菜单按钮。默认库 `data/miniapp-test.db`，指向生产库或库中已有业务数据时拒绝启动。日志带 `[api]`/`[vite]`/`[tunnel]` 前缀 |

### 测试

```sh
GOTOOLCHAIN=go1.27.3+auto go test ./...
GOTOOLCHAIN=go1.27.3+auto go test -race ./...
cd web && npm run check && npm test && npm run build
```
