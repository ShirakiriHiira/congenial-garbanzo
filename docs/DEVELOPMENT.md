# 开发规范

后端权威开发规范。HTTP 字段清单见 [API.md](API.md)，前端规范见 [MINI_APP_DEVELOPMENT.md](MINI_APP_DEVELOPMENT.md)。

## 1. 定位与范围

通用服务与订阅到期管理器：一个服务可有多个有效期，每个有效期可配置提醒规则、约定条款和续期历史。

不做：自动扣款、自动续费、外部账号集成、支付、动态字段引擎、Redis/PostgreSQL/ORM/微服务。备注只存普通说明，不保存密码、API Token 或私钥。

## 2. 领域模型

| 实体 | 关系与语义 |
| --- | --- |
| User | Telegram 用户、时区、私聊绑定；数据按用户隔离 |
| Category | 内置（`sim`/`vps`/`subscription`/`domain`/`other`）或单用户自定义；名称仅展示 |
| ServiceItem | 属于用户与类别；状态 `active`/`archived`；`identifier` 仅非 SIM 类别可用（SIM 由触发器固定为 NULL） |
| SIMDetails | 仅 SIM：卡类型、号码、国家/地区、ICCID；与 ServiceItem 一对一 |
| Period | 属于服务；名称可空、开始可空、截止必填、日期精度、用户时区、`cycle` |
| PeriodTerms | 每有效期一行；`once`/`recurring`/`custom`、周期、金额、币种、`revision` |
| ReminderRule | 属于有效期；提前天数、提醒时刻、启用状态、`revision` |
| Renewal | 属于有效期；续期前后截止快照、周期、金额/币种、备注 |
| ReminderJob | 属于有效期与规则；周期、规则版本、计划时间、到期快照、租约、重试、结果 |
| BotDialog | 每用户最多一条向导草稿；15 分钟 TTL；动作/步骤白名单 |

```text
User 1─N ServiceItem 1─N Period 1─1 PeriodTerms
                      └1─1 SIMDetails (仅 sim)
Period 1─N ReminderRule 1─N ReminderJob
Period 1─N Renewal
User 1─0..1 BotDialog
```

- 有效期状态不落库，读取时由 `expires_at` 计算为 `active`/`expired`。
- 主有效期：未过期中最早的一条；全部过期时取最近过期的一条；同截止取最大 ID。首页逐条展示全部有效期，统计与筛选仍用主有效期。

## 3. 架构与代码组织

技术栈：Go 1.27.3、`net/http`、`database/sql`、`modernc.org/sqlite`；Svelte 5 + Vite + TypeScript（原生 CSS）；版本化 SQL migration；时区数据嵌入二进制。单实例单写者：SQLite 连接池限一个连接，不用分布式锁。

| 目录 | 职责 |
| --- | --- |
| `internal/api` | JSON API、initData 鉴权、严格解析、状态码映射 |
| `internal/auth` | initData 校验 |
| `internal/bot` | Telegram 命令/回调/向导与消息渲染；只调用 `internal/service` |
| `internal/config` | 环境配置 |
| `internal/domain` | 时间、精度、提醒时刻计算 |
| `internal/reminder` | 调度循环、发送、重试/补发 |
| `internal/service` | 领域规则、授权、事务、乐观并发 |
| `internal/storage` | SQLite 连接、迁移、任务扫描/租约/写回 |
| `web/` | Mini App 页面、路由、表单、API 客户端；不复制业务规则 |
| `cmd/miniapp-test` | 隔离测试 API、临时库、假签名；不启动 Bot/投递 |
| `migrations/` | 唯一基线 `001_initial.sql` |
| `scripts/` | 本地测试/联调脚本 |
| `main.go` | 启动装配、HTTP、Bot、调度器、优雅关闭 |

## 4. 关键规则

**错误与校验**：业务错误只用 `invalid_input`、`not_found`、`conflict`、`forbidden`、`unavailable`，映射为稳定 HTTP 状态与通用消息；只接受单个 JSON 对象与 `application/json`，未知字段、重复查询参数、非法 ID 一律拒绝；Bot 回调数据只含动作与十进制 ID。

**并发**：有效期 PATCH/DELETE 用 `expected_cycle`，条款用 `expected_terms_revision`，规则用 `expected_revision`；冲突返回 409 或 Bot 提示并保留草稿，不自动重试。

**时间**：内部 UTC。`date` 是用户时区当地日历日结束的排他截止，`datetime` 是带 offset 的 RFC3339。Bot 向导接受本地 `YYYY-MM-DD HH:MM`（`domain.ParseLocalDateTime`），也接受 RFC3339；DST 跳过的时间拒绝，重复取较早。改时区时 `date` 重算、`datetime` 保持绝对时刻。

**金额**：金额与币种必须成对；API 金额为非负整数最小货币单位，Mini App 输入小数并按 ISO 4217 小数位换算；币种为三位大写字母。提醒规则必须早于截止。

**投递**：幂等键 `(period_id, rule_id, cycle, rule_revision)`，成功发送按 `(period_id, rule_id, expires_at_snapshot)` 去重；补发窗口、有限重试、租约、fencing token 与取消竞态语义保留，不承诺 exactly-once。

**安全**：备注、日志、URL、回调、截图不得含密码、API Token、私钥、initData、完整号码或 ICCID；Mini App 不直接展示后端消息，错误映射为受控中文文案；永久删除不可逆，前端与 Bot 必须二次确认并说明级联范围。

## 5. Telegram 交互约定

- 只保留 7 个入口：`/start`、`/help`、`/services`、`/add`、`/reminders`、`/settings`、`/cancel`；不接受用户输入任何 ID、`key=value` 或周期编号。
- 向导动作白名单：`service_add`、`service_edit`、`period_add`、`period_edit`、`rule_add`、`rule_edit`、`renew`、`timezone`；每用户最多一个 15 分钟草稿，读取命令不清草稿。
- 回调就地改写原消息并显式清空 `reply_markup`，否则会留下失效按钮；`message is not modified` 视为成功，其他编辑失败降级为新消息。提醒卡片按钮走 `notify` 作用域，不改写正文。正文超 3800 字改为新消息。
- 回调必须恰好回执一次（否则按钮转圈不消失）；回执文案是 Toast，只有聊天里没有任何输出时才带文案，聊天已显示结果时只做静默回执，不弹「xx已发送」。
- 配置 `MINIAPP_PUBLIC_URL` 时，`/start` 为用户私聊安装 `web_app` 菜单按钮并记入 `users.menu_button_url`；同名 URL 不重复调用，失败只记日志，下次 `/start` 重试。

## 6. 数据库与迁移

- 基线 `migrations/001_initial.sql`；实体表统一 `INTEGER PRIMARY KEY AUTOINCREMENT`，避免回调 ID 复用。
- 外键 `ON DELETE CASCADE`（归属关系除外）：服务删除级联有效期、条款、规则、任务、续期历史、对话；有效期级联条款、规则、任务、历史；规则级联任务。
- `period_terms` 每有效期一行，创建时写入 `mode=custom`、`revision=1`；`service_periods` 不存状态列。
- `schema_migrations` 保存版本与 SHA-256 checksum；旧库或 checksum 不匹配即启动失败，不自动迁移、删库或降级。
- 变更流程：新增版本化 migration，禁止改已应用版本；重建基线时记录并归档旧库（只重命名）。
- 开发/测试/生产数据严格分离：测试用临时目录，生产升级用新空卷；禁止自动删库、删表、清空 sequence。
- `bot_dialogs.action` 白名单固定在 schema，步骤名由代码注册表校验。

## 7. 测试与验收

```sh
GOTOOLCHAIN=go1.27.3+auto go test ./...
GOTOOLCHAIN=go1.27.3+auto go test -race ./...
cd web && npm run check && npm test && npm run build
./scripts/test-miniapp.sh
```

- 迁移/存储：空库初始化、旧 schema 拒绝、checksum 不匹配拒绝、失败回滚、外键级联、AUTOINCREMENT 高水位保留。
- API：严格校验、新字段、删除/归档语义、跨用户 404、版本冲突 409、分页。
- Bot：新命令面、无 ID 交互、回调前缀 `v3:`、旧按钮拒绝、删除确认、续期冲突、只读命令不清草稿。
- Mini App：五类场景、空/错误/只读/冲突状态、无内部版本展示。
- 移动端：320/375/430 CSS px 无横向溢出，触控区 ≥44×44，双主题与安全区。

验收前跑冒烟路径：创建服务 → 添加有效期 → 续期 → 添加规则 → 删除服务，并观察实际界面与数据库结果。

`scripts/test-miniapp.sh`：假签名 API + Vite，不启动 Bot 轮询或投递，不读 `.env` 与生产库，Ctrl-C 删除临时库。
`scripts/dev-miniapp.sh`：真实联调（Bot 轮询、调度器、菜单按钮），默认 `data/miniapp-test.db`，检测到生产库或已有业务数据时拒绝启动，除非 `MINIAPP_ALLOW_EXISTING_DB=1`。

## 8. Git 提交规范

- 标题 `type: 简明英文描述`，默认单行，不加正文与脚注。
- 类型：`feat` `fix` `refactor` `docs` `test` `style` `build` `ci` `perf` `chore`。
- 未明确要求不写 issue 编号或 `Closes #xxxx`；确需关闭 issue 时按要求附加。
- 不添加未经要求的描述、测试报告或协作署名。
