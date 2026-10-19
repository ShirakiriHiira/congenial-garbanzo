# 服务与订阅到期提醒

单实例 Go 服务与 Telegram Bot：管理 SIM/eSIM、VPS 与云服务器、域名、软件与 AI 订阅，以及其他有期限的服务。每个服务可有多个有效期；每个有效期可配置提醒规则、约定条款和续期历史。SQLite 是业务事实来源，Svelte Mini App 使用 Telegram `initData` 鉴权。

明确不做：自动扣款、自动续费、外部服务账号集成、支付、动态字段引擎、Redis/PostgreSQL/ORM/微服务。备注只存普通说明，不保存密码、API Token 或私钥。

## 主要功能

- 服务、内置/自定义类别、多个有效期。
- SIM 专属资料：卡类型、号码、国家/地区、ICCID。
- 通用服务字段：服务商、公开标识、管理链接、备注、类别。
- 有效期条款：一次性、周期性、暂不设置；Mini App 金额可输入小数，按 ISO 4217 币种小数位换算为整数最小货币单位传输，币种为三位大写字母。
- 每用户时区的提醒规则；提前天数、提醒时刻、启用/禁用。
- 显式确认续期，保留续期前后截止与付款记录；不自动推进日期。
- Telegram 分步向导与 Inline Keyboard；Mini App 同源页面。
- 归档/恢复与永久级联删除，删除均需二次确认。
- 提醒任务租约、有限重试、补发窗口与去重；不承诺 exactly-once。

## 使用场景与字段映射

| 类别 | 服务商字段 | 公开标识字段 | 额外字段 |
| --- | --- | --- | --- |
| SIM/eSIM | 运营商 | 公开标识 | 卡类型、号码、国家/地区、ICCID |
| VPS/云服务器 | 服务商 | 主机名或 IP | 无 |
| 域名 | 注册商 | 域名 | 无 |
| 软件/AI 订阅 | 服务商 | 账号标识 | 无 |
| 其他/自定义 | 服务商 | 公开标识 | 无 |

金额与币种属于有效期条款和续期历史，不属于服务主表。状态筛选中的 `active` 表示所有未归档服务，包含已过期和临期服务。

## 技术栈

Go 1.27.1、`net/http`、`database/sql`、`modernc.org/sqlite`（CGO 关闭可构建）、Telegram Bot SDK；Svelte 5 + Vite + TypeScript；版本化 SQL migration；测试使用 `go test`、`vitest` 与 `svelte-check`。

## 快速开始

```sh
cp .env.example .env
chmod 600 .env
# 在 .env 中设置 TELEGRAM_BOT_TOKEN，并导出白名单：
export ALLOWED_TELEGRAM_USER_IDS='10001,10002'
CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o ./bin/remind .
./bin/remind
```

本地当前目录 `.env` 中非空 `TELEGRAM_BOT_TOKEN` 优先于进程环境；其他配置只读进程环境。白名单不能为空，不提供开放注册。生产容器通过环境变量注入配置，不挂载 `.env`。

健康检查：`curl -fsS http://127.0.0.1:8080/readyz` 或 `./bin/remind healthcheck`。`healthcheck` 只使用 `HTTP_ADDR`，不加载 Token、不打开数据库。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | 无（必填） | Bot Token；本地可由 `.env` 提供 |
| `ALLOWED_TELEGRAM_USER_IDS` | 无（必填） | 逗号分隔的正整数 Telegram 用户 ID |
| `DATABASE_PATH` | `./data/remind.db` | 持久 SQLite 文件；不接受内存库或 `file:` URI |
| `APP_TIMEZONE` | `Asia/Taipei` | 新用户默认 IANA 时区 |
| `REMINDER_SCAN_INTERVAL` | `30s` | 调度扫描间隔 |
| `REMINDER_CATCHUP_WINDOW` | `24h` | 补发窗口 |
| `WEBAPP_AUTH_MAX_AGE` | `24h` | 用户未在 Mini App 设置会话有效期时的 `initData` 默认有效期 |
| `HTTP_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `TELEGRAM_API_PROXY` | 空 | Telegram API 代理；空值沿用 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` |
| `MINIAPP_HTTP_PORT` | `8081` | Compose 中 web 容器的本机回环端口 |
| `REMIND_DATA_VOLUME` | `sim-reminder-data` | Compose 命名卷 |

Mini App 设置页可选 1 小时、6 小时、24 小时或 7 天；选择保存在用户记录中，未选择时使用 `WEBAPP_AUTH_MAX_AGE`。

## Telegram Bot

只保留 7 个入口，不接受用户输入任何 ID、`key=value`、周期编号或内部版本。

| 命令 | 行为 |
| --- | --- |
| `/start`、`/help` | 欢迎/帮助；带“我的服务 / 添加服务 / 提醒任务 / 设置”；有草稿时显示“继续当前流程” |
| `/services [关键词]` | 服务概览：统计、状态/类别筛选、分页；关键词只搜索名称、服务商、公开标识 |
| `/add` | 新建服务向导 |
| `/reminders` | 提醒任务；默认待发送，可切换失败/全部并取消 |
| `/settings` | 时区、私聊绑定状态、修改时区 |
| `/cancel` | 取消当前向导，不写入草稿中的修改 |
| `/help` | 同 `/start` 的帮助 |

未知命令只回复帮助，不执行写操作。所有对象从列表/详情按钮进入：服务详情可添加/编辑有效期、编辑/归档/恢复/删除服务；有效期详情可续期、管理规则、查看续期历史、编辑/删除有效期；规则详情可编辑、启用/禁用、删除；提醒任务可取消 `pending`/`sending`/`failed`。

向导动作固定为 `service_add`、`service_edit`、`period_add`、`period_edit`、`rule_add`、`rule_edit`、`renew`、`timezone`。每用户最多一个 15 分钟草稿；读取命令不清除草稿；发起另一个写流程前会要求“继续当前 / 取消并开始”。删除/归档/恢复均需二次确认并说明级联范围。

示例：

```text
/add → 选择类别 → 输入名称 → 跳过剩余可选资料 → 确认创建
服务详情 → 添加有效期 → 名称 → 截止方式 → 截止 → 条款 → 确认创建
有效期详情 → 续期 → 新截止 → 金额（可跳过）→ 备注（可跳过）→ 确认续期
```

## HTTP API

所有业务路由位于 `/api/v1`，要求 `Authorization: tma <initData>`。只接受严格 JSON（`application/json`）、单对象、未知字段拒绝；ID 为十进制字符串；写操作不自动重试。错误形状：

```json
{"error":{"code":"invalid_input","message":"Invalid request."}}
```

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET, PATCH | `/me` | 当前用户；PATCH 时区或 `session_ttl_seconds` |
| GET, POST | `/categories` | 列出/创建本人自定义类别 |
| PATCH | `/categories/{id}` | 改本人自定义类别名 |
| GET, POST | `/services` | 列表/创建 |
| GET | `/services/overview` | counts + 服务/主有效期与全部有效期摘要分页 |
| GET, PATCH | `/services/{id}` | 读取/更新；PATCH `status=archived` 归档，`status=active` 恢复 |
| DELETE | `/services/{id}` | 永久级联删除 |
| GET, POST | `/services/{id}/periods` | 列表/创建有效期 |
| GET, PATCH, DELETE | `/periods/{id}` | 读取/更新/永久删除；PATCH 与 DELETE 均需 `expected_cycle` |
| GET, POST | `/periods/{id}/rules` | 列表/创建规则 |
| GET, PATCH, DELETE | `/rules/{id}` | 读取/更新/永久删除；PATCH 需 `expected_revision`，DELETE 用查询参数 `expected_revision` |
| GET, POST | `/periods/{id}/renewals` | 续期历史/确认续期 |
| GET | `/reminders` | 提醒任务列表（`status`、`limit`、`after_id`） |
| GET | `/reminders/{id}` | 任务详情 |
| POST | `/reminders/{id}/cancel` | 取消可取消任务 |

列表使用 keyset 分页：`limit` 1–100，`after_id` 可选；响应 `{"data":[...],"next_cursor":null|"id"}`。

精确字段：

- Service：`id,user_id,name,category_id,provider,identifier,management_url,notes,status,created_at,updated_at,sim_details`；`sim_details` 仅 SIM 非空，含 `card_type,phone_number,country_or_region,iccid`。
- Period：`id,service_id,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,terms,created_at,updated_at`；`status` 只由截止时间计算为 `active`/`expired`。
- Terms：`mode,interval_count,interval_unit,amount,currency,revision`。
- Rule：`id,period_id,days_before,reminder_time,enabled,revision,created_at,updated_at`。
- Renewal：`id,period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at`。
- Job：`id,period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,sent_at,telegram_message_id,last_error_code,created_at,updated_at`。
- User：`id,telegram_user_id,timezone,has_private_chat,session_ttl_seconds,created_at,updated_at`。
- Category：`id,user_id,code,name,kind,created_at,updated_at`。

创建有效期必须提供 `date_precision` 与 `expires_at`；`name`、`starts_at`、`terms` 可选。PATCH 有效期必须带 `expected_cycle`；包含 `terms` 时必须带 `expected_terms_revision`；`terms:null` 重置为 `mode=custom` 并递增 revision。规则创建未提供 `enabled` 时默认为 `true`；`(period_id,days_before,reminder_time)` 唯一。续期必须显式提交新截止，并使用 `expected_cycle`；相同周期相同参数重放返回既有历史（200）。

状态筛选为 `active,due_soon,expired,no_period,archived,all`。`due_soon` 按用户时区日历日 0–7 天。主有效期优先选择未过期中最早的一条；若全部过期，选择最近过期的一条；同截止取最大 ID。Mini App 首页逐条展示服务的全部有效期；统计与筛选仍按主有效期。

## 时间、条款与提醒

- 内部时间 UTC。`date` 表示用户时区当地日历日结束的排他截止；`datetime` 是带 offset 的 RFC3339。
- 提前天数按当地日历计算；DST 不存在时刻顺延，重复时刻取较早候选。
- 任务幂等键 `(period_id, rule_id, cycle, rule_revision)`；成功发送按 `(period_id, rule_id, expires_at_snapshot)` 去重。
- 补发窗口、有限重试、租约、fencing token、取消/在途发送竞态语义保留；不承诺 exactly-once。
- 修改时区：`date` 保留当地日期并重算 UTC 截止；`datetime` 保持绝对时刻；相关有效期 `cycle+1`，未发送任务取消。
- 恢复归档服务：相关有效期 `cycle+1`，旧按钮/旧草稿失效；历史和已发送记录保留。
- 条款只记录约定，不自动扣款、不推算日期、不代表已付款。

## Docker 部署

```sh
docker compose build
docker compose up -d
curl -fsS http://127.0.0.1:8081/app/
```

- `app` 使用 distroless 非 root 镜像，SQLite 位于命名卷 `/data`；`web` 使用 nginx 非特权镜像并在同源路径 `/app/` 提供静态资源与 `/api/`、`/healthz`、`/readyz` 代理。
- 两个服务均只绑定本机回环端口；对外 HTTPS 由反向代理或隧道终止。
- 备份时停止写入（或至少停止 app）并复制数据库及 `-wal`/`-shm`；恢复时整体还原。
- 禁止 `docker compose down -v`、删表、清库或重置 sequence。

## 数据与迁移

- 干净基线：`migrations/001_initial.sql` 建立全部表、约束、索引与触发器。
- `schema_migrations` 保存版本与 SHA-256 checksum。旧库或 checksum 不匹配会导致启动失败，不会自动迁移、删库或降级。
- 升级必须使用新的空数据库路径/卷；旧库只保留备份，不原地复用。
- 永久删除会级联：服务 → 有效期、条款、规则、任务、续期历史、对话；有效期 → 条款、规则、任务、历史；规则 → 任务。
- 开发库 `data/*.db` 不应提交；测试脚本只使用临时目录。

## 开发与测试

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o /tmp/remind-clean .

cd web
npm ci
npm run check
npm test
npm run build
```

`./scripts/test-miniapp.sh`：需要 Go 1.27.1、Node 24、npm、curl。先检查 8088/5180 端口；使用 `cmd/miniapp-test`、临时 SQLite、真实 service/storage、假签名，不启动 Bot 轮询或 Telegram 投递，不读取 `.env` 或生产数据库。打开 `http://127.0.0.1:5180/app/`；Ctrl-C 删除临时数据库。仅该隔离进程提供 `GET /__test/init-data`；不得通过隧道公开测试端口。

`./scripts/dev-miniapp.sh`：真实联调脚本，副作用明确：读取真实 Bot Token、启动 Bot 轮询与提醒调度器、通过 Telegram API 设置菜单按钮、启动 Go API（8080）与 Vite（5173），默认使用 `data/miniapp-test.db`。若目标路径是 `data/remind.db`，或已有数据库含任意 `service_items`/旧 `sims` 行，则拒绝启动；只有显式设置 `MINIAPP_ALLOW_EXISTING_DB=1` 才继续。该脚本不是纯前端测试，禁止指向生产库。

## 文档索引

- [开发规范](docs/DEVELOPMENT.md)：领域模型、架构边界、数据库与测试规则。
- [Mini App 专项规范](docs/MINI_APP_DEVELOPMENT.md)：路由、交互、移动端与验收清单。
- `test_screen/`：当前 Mini App 截图回归参考。
- `docs/miniapp.png`、`docs/miniapp_light.png`：视觉参考。

## Git Commit Convention

`type: concise English description`，类型包括 `feat,fix,refactor,docs,test,style,build,ci,perf,chore`。每次只提交逻辑相关内容，不提交密钥、用户数据或临时文件；提交前检查 diff 并运行相关测试。未经明确授权不执行 commit、amend、reset 或历史重写。
