# 开发规范

本文件是权威开发规范。HTTP 字段清单只在 `README.md` 维护，这里不重复精确定义。

## 1. 产品定位与边界

通用服务与订阅到期管理器。一个服务可有多个有效期；每个有效期可配置提醒规则、约定条款和续期历史。明确不做自动扣款、自动续费、外部账号集成、支付、动态字段引擎、Redis/PostgreSQL/ORM/微服务。备注只存普通说明，不保存密码、API Token 或私钥。

## 2. 领域模型

| 实体 | 关键关系与语义 |
| --- | --- |
| User | Telegram 用户、时区、私聊绑定；所有数据按用户隔离 |
| Category | 内置类别（`sim`/`vps`/`subscription`/`domain`/`other`）或单用户自定义 generic 类别；类别名称只用于展示 |
| ServiceItem | 属于用户与类别；状态 `active`/`archived`；`provider`、`identifier`、`management_url`、`notes` 承载通用类型信息 |
| SIMDetails | 仅 SIM 类别：卡类型、号码、国家/地区、ICCID；与 ServiceItem 一对一 |
| Period | 属于服务；名称可空、开始可空、截止必填、日期精度、用户时区、`cycle` |
| PeriodTerms | 每个有效期始终一行；`once`/`recurring`/`custom`、周期、金额、币种、独立 `revision` |
| ReminderRule | 属于有效期；提前天数、提醒时刻、启用状态、`revision` |
| Renewal | 属于有效期；续期前后截止快照、周期、金额/币种、备注 |
| ReminderJob | 属于有效期与规则；周期、规则版本、计划时间、到期快照、租约、重试、结果 |
| BotDialog | 每用户最多一条持久向导草稿；15 分钟 TTL；动作/步骤白名单 |

关系：

```text
User 1─N ServiceItem 1─N Period 1─1 PeriodTerms
                      └1─1 SIMDetails (仅 sim)
Period 1─N ReminderRule 1─N ReminderJob
Period 1─N Renewal
User 1─0..1 BotDialog
```

有效期状态不落库，读取时由 `expires_at` 与当前时间计算为 `active`/`expired`。主有效期优先选择未过期中最早的一条；全部过期时选择最近过期的一条；同截止取最大 ID。

## 3. 技术栈与架构

- Go 1.27.1、`net/http`、`database/sql`、`modernc.org/sqlite`。
- Svelte 5 + Vite + TypeScript；原生 CSS、Telegram 主题与 BackButton。
- 版本化 SQL migration；IANA 时区数据嵌入二进制。
- 单实例、单写者：SQLite 连接池限制为一个连接；不引入分布式锁。

## 4. 职责边界

| 模块 | 职责 |
| --- | --- |
| `internal/bot` | Telegram 传输校验、Inline Keyboard、分步向导、消息渲染；只调用 `internal/service` |
| `internal/api` | JSON API、initData 鉴权、严格 JSON/查询解析、状态码映射 |
| `internal/service` | 领域规则、授权、事务、乐观并发、时间/条款/续期语义 |
| `internal/storage` | SQLite 连接、迁移、提醒任务扫描/租约/结果写回 |
| `internal/reminder` | 调度循环、Telegram 发送、重试/补发 |
| `web/` | Mini App 页面、路由、表单、API 客户端；不复制业务规则 |
| `cmd/miniapp-test` | 隔离测试 API、临时数据库、假签名；不启动 Bot/投递 |
| `main.go` | 启动装配、HTTP、Bot、调度器、优雅关闭 |

## 5. 代码组织

```text
internal/api        HTTP 边界
internal/auth       initData 校验
internal/bot        Telegram 命令/回调/向导
internal/config     环境配置
internal/domain     时间、精度、提醒时刻计算
internal/reminder   调度与发送
internal/service    领域服务
internal/storage    SQLite 与迁移
migrations          单一干净基线 001_initial.sql
web/                Mini App
cmd/miniapp-test    隔离测试 API
scripts/            本地测试/联调脚本
```

## 6. 错误、校验、并发与安全

- 业务错误使用固定 code：`invalid_input`、`not_found`、`conflict`、`forbidden`、`unavailable`；API 映射为稳定 HTTP 状态与通用消息。
- 严格 JSON：只接受单对象与 `application/json`，未知字段、重复查询参数、非法 ID 均拒绝。Bot 回调数据只含动作与十进制 ID。
- 乐观并发：有效期 PATCH/DELETE 使用 `expected_cycle`；条款使用 `expected_terms_revision`；规则使用 `expected_revision`。冲突返回 409 或 Bot 提示，保留草稿，不自动重试。
- 时间：内部 UTC；`date` 为当地日历日结束排他截止；`datetime` 为带 offset 的 RFC3339。修改时区时 `date` 重算、`datetime` 保持绝对时刻。
- 提醒规则必须早于截止；金额与币种必须成对；API 金额为非负整数最小货币单位，Mini App 输入小数并按 ISO 4217 币种小数位换算；币种三位大写字母。
- 备注、日志、URL、回调、截图不得包含密码、API Token、私钥、initData、完整号码或 ICCID。
- Mini App 不信任后端消息直接展示；错误统一映射为受控中文文案。
- 永久删除不可逆，前端与 Bot 必须二次确认并说明级联范围。

## 7. 数据库与迁移

- 干净基线：`migrations/001_initial.sql`。所有实体表使用 `INTEGER PRIMARY KEY AUTOINCREMENT`，避免回调 ID 复用。
- 外键统一 `ON DELETE CASCADE`，用户与类别归属关系除外；服务删除级联有效期、条款、规则、任务、续期历史、对话；有效期删除级联条款、规则、任务、历史；规则删除级联任务。
- `period_terms` 每个有效期始终一行；创建时写入 `mode=custom`、`revision=1`。
- `service_periods` 无状态列；API 状态由截止时间计算。
- `schema_migrations` 保存版本与 SHA-256 checksum。旧库/checksum 不匹配时启动失败，不自动迁移、删库或降级。
- schema 变更流程：新增版本化 migration；禁止修改已应用版本；开发阶段需要重建基线时明确记录并归档旧库（仅重命名，不删除）。
- 开发/测试/生产数据严格分离：测试使用临时目录；生产升级使用新空卷/新路径；禁止自动删库、删表、清空 sequence。
- `bot_dialogs.action` 白名单在 schema 中固定，步骤名由代码注册表校验。

## 8. 测试与验收

- Go：`go test ./...`、`go test -race ./...`、`CGO_ENABLED=0 go build -trimpath`。
- 前端：`npm run check`、`npm test`、`npm run build`。
- 迁移/存储：空库初始化、旧 schema 拒绝、checksum 不匹配拒绝、失败回滚、外键级联、AUTOINCREMENT 高水位保留。
- API：严格校验、新字段、删除/归档语义、跨用户 404、版本冲突 409、分页。
- Bot：新命令面、无 ID 交互、回调前缀 `v3:`、旧按钮拒绝、删除确认、续期冲突、只读命令不清草稿。
- Mini App：SIM/VPS/域名/订阅/自定义类别、四类场景、空/错误/只读/冲突、无内部版本展示。
- 移动端：320/375/430 CSS px 无横向溢出；触控区不小于 44×44；双主题与安全区。
- 验收前运行冒烟路径：创建服务 → 添加有效期 → 续期 → 添加规则 → 删除服务，并观察实际界面/数据库结果。

常用命令：

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
cd web && npm run check && npm test && npm run build
./scripts/test-miniapp.sh
```

`scripts/dev-miniapp.sh` 会启动真实 Bot 轮询、调度器并设置菜单按钮；默认 `data/miniapp-test.db`，检测到生产库或已有业务数据时拒绝启动，除非 `MINIAPP_ALLOW_EXISTING_DB=1`。

## 9. Git Commit Convention

`type: concise English description`，类型包括 `feat,fix,refactor,docs,test,style,build,ci,perf,chore`。每次只提交逻辑相关内容，不提交密钥、用户数据或临时文件。未经明确授权不执行 commit、amend、reset 或历史重写。
