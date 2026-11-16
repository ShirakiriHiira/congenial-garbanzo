# 服务与订阅到期提醒

Telegram Bot + Mini App：用于管理 SIM/eSIM、VPS、域名、软件与 AI 订阅等有期限的服务，基于用户时区提供到期提醒。

- **后端**: Go 1.27.3 + SQLite（单实例、单写者）。
- **前端**: Svelte 5 + Vite + TypeScript。
- **文档**: [开发规范](docs/DEVELOPMENT.md) | [HTTP API](docs/API.md) | [Mini App 规范](docs/MINI_APP_DEVELOPMENT.md)

## 部署

### Docker Compose
```sh
cp .env.example .env
chmod 600 .env
# 编辑 .env，填写 TELEGRAM_BOT_TOKEN 与 ALLOWED_TELEGRAM_USER_IDS
docker compose pull
docker compose up -d
```

### 环境配置
核心环境变量：
- `TELEGRAM_BOT_TOKEN`: 必填，机器人 Token。
- `ALLOWED_TELEGRAM_USER_IDS`: 必填，逗号分隔的 Telegram 用户 ID 白名单。
- `APP_TIMEZONE`: 默认 `Asia/Taipei`。
- `MINIAPP_PUBLIC_URL`: 选填，Mini App 公网 HTTPS 地址（用于启用菜单按钮）。

## 开发与调试

依赖：Go 1.27.3, Node 24。

- **后端**: 运行 `./bin/remind`。
- **前端**: 在 `web/` 目录下运行 `npm run dev`。
- **本地调试脚本**:
  | 脚本 | 用途 |
  | --- | --- |
  | `./scripts/test-miniapp.sh` | 模拟环境：无需真实 Token，使用独立测试库。 |
  | `./scripts/dev-miniapp.sh` | 真实环境：需配置真实 Token，包含完整机器人功能。 |

## 测试
```sh
# 后端
go test ./...
# 前端
cd web && npm run check && npm test
```
