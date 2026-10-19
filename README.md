# 服务与订阅到期提醒

单实例 Go 服务与 Telegram Bot，用于管理 SIM/eSIM 和通用服务项目、多有效期、提醒规则、续期历史与显式确认续期。SQLite 是业务事实来源。Svelte Mini App 使用 Telegram `initData` 鉴权。周期条款仅记录约定，不自动扣款、推算到期日或推断付款。见[总体开发规范](docs/DEVELOPMENT.md)与[Mini App 专项规范](docs/MINI_APP_DEVELOPMENT.md)。

## 本地运行

要求 Go **1.27.1**。SQLite 使用纯 Go `modernc.org/sqlite v1.60.1`；生产构建不需要 CGO。迁移与 IANA 时区数据嵌入二进制。

```sh
cp .env.example .env
chmod 600 .env
# 在 .env 中设置 TELEGRAM_BOT_TOKEN。
export ALLOWED_TELEGRAM_USER_IDS='10001,10002'
CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o ./bin/remind .
./bin/remind
```

请将示例白名单换成真实 Telegram 用户 ID。白名单不能为空，不提供开放注册模式。本地当前目录 `.env` 中非空 `TELEGRAM_BOT_TOKEN` 优先于进程环境；其他配置只读进程环境。不要把不可信 `.env` 当 shell 脚本执行。生产容器通过环境变量注入配置，不挂载 `.env`。

使用 `curl -fsS http://127.0.0.1:8080/readyz` 或 `./bin/remind healthcheck` 检查就绪状态。下列启动错误都会使进程以 1 退出。

启动失败时进程以 1 退出，`error` 字段给出具体原因：

| `error` 内容 | 处理 |
| --- | --- |
| `invalid configuration: TELEGRAM_BOT_TOKEN is required` | `.env` Token 为空且进程环境未设置该变量 |
| `invalid configuration: ALLOWED_TELEGRAM_USER_IDS ...` | 未 `export` 白名单，或含非正整数/空项 |
| `invalid configuration: DATABASE_PATH/APP_TIMEZONE/...` | 对应环境变量不合法；这些变量不读 `.env` |
| `telegram initialization failed: ...` | Token 被 Telegram 拒绝，或本机无法访问 `api.telegram.org`（代理、DNS、TLS） |
| `telegram identity unavailable: ...` | 同上，`GetMe` 未返回有效 Bot 用户名 |
| `storage unavailable: ...` / `migration failed: ...` | 数据库路径不可写或迁移校验失败 |
| `http listener unavailable: ...` | `HTTP_ADDR` 端口被占用或地址非法 |

`healthcheck` 仅使用 `HTTP_ADDR` 请求本机 `/readyz`，2 秒超时；不加载 Token、不打开数据库，非 200 时非零退出。通配监听地址转换为回环地址。Go 只读 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`，不读桌面系统代理；需要本机代理时显式设置 `TELEGRAM_API_PROXY`，例如 `http://127.0.0.1:7890` 或 `socks5://127.0.0.1:7891`。该代理只作用于 Telegram API。

## Mini App 本地测试

运行 `./scripts/test-miniapp.sh`，浏览器打开 `http://127.0.0.1:5180/app/`。需要 Go 1.27.1、Node 24、npm、curl。脚本启动独立 `cmd/miniapp-test`，使用临时 SQLite、真实 service/storage 代码和 SIM/通用样例，不启动 Bot 轮询或 Telegram 投递。UI 绑定 `127.0.0.1:5180`、API 绑定 `127.0.0.1:8088`；Ctrl-C 清理临时数据库，不读取生产配置/数据库。

仅该隔离测试进程提供 `GET /__test/init-data`，使用每进程随机假 Bot key 签名；业务请求仍经过正常鉴权。前端测试模式须同时满足开发构建与 `VITE_MINIAPP_TEST_MODE=true`。生产无测试路由或鉴权绕过，不得通过隧道公开测试端口。

真实 Telegram Mini App 需要 HTTPS 同源代理；生产 `initData` 由 Telegram 注入，默认 5 分钟过期。过期后重新打开 Mini App，不得放宽期限。下文含真实 HTTPS 调试步骤。

### 真实 Mini App 联调

Telegram 官方只在测试环境允许 HTTP 链接；本服务 Bot 轮询没有接入测试 API 基址。因此用现有 Bot 调试必须为本机提供 HTTPS 入口，可使用 Cloudflare Tunnel。不要改 `WEBAPP_AUTH_MAX_AGE` 掩盖过期问题。

有 Cloudflare 自定义域名时，将隧道 origin 指向 `http://127.0.0.1:5173`：

```sh
MINIAPP_PUBLIC_URL='https://sim.example.com' \
TELEGRAM_BOT_TOKEN='从受控环境提供' \
ALLOWED_TELEGRAM_USER_IDS='实际Telegram用户ID' \
./scripts/dev-miniapp.sh
```

无自定义域名时省略 `MINIAPP_PUBLIC_URL`，脚本会启动 Cloudflare 快速隧道。脚本检查/准备 Node 24、运行 `npm ci` 与 `go build`，启动 Go API `127.0.0.1:8080` 和 Vite `127.0.0.1:5173`（`/api` 代理至 Go），随后设置 Bot 菜单按钮指向 HTTPS `/app/`。Ctrl-C 停止全部进程。`MINIAPP_PUBLIC_URL` 必须为纯 origin（可带 `/app`），不能有其他路径或查询串；Cloudflare Public Hostname 指向 `http://127.0.0.1:5173`。

若自动设置菜单按钮失败，脚本会打印 URL，可在 BotFather 执行 `/setmenubutton` 设置 `https://<域名或随机隧道>/app/`。手动开隧道：

```sh
cloudflared tunnel --url http://127.0.0.1:5173
```

隧道必须指向 5173，不能指向 8080，否则无静态资源。先私聊 Bot `/start` 绑定私聊接收地址；Mini App 鉴权不会绑定私聊。白名单外返回 403。显示「会话已过期」时重新打开 Mini App。调试真实业务会读写数据库并启动调度器，不要用生产卷，不要同时启动两个使用相同 Bot Token 的轮询实例。

## 配置

| 环境变量 | 默认值 | 用途与限制 |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | 无 | 必填；本地 `.env` 优先规则见上文 |
| `ALLOWED_TELEGRAM_USER_IDS` | 无 | 必填，逗号分隔正 int64 ID |
| `DATABASE_PATH` | `./data/remind.db` | 文件型 SQLite，正式服务不能用内存库 |
| `APP_TIMEZONE` | `Asia/Taipei` | 新用户默认 IANA 时区，不覆盖现有用户设置 |
| `REMINDER_SCAN_INTERVAL` | `30s` | Go duration，至少 1 秒 |
| `REMINDER_CATCHUP_WINDOW` | `24h` | 正 duration，限制离线补发窗口 |
| `WEBAPP_AUTH_MAX_AGE` | `5m` | initData 最大年龄 |
| `HTTP_ADDR` | `127.0.0.1:8080` | 容器内固定监听 `:8080` |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `TELEGRAM_API_PROXY` | 无 | Telegram API 出口代理，可用 http/https/socks5/socks5h |
| `REMIND_DATA_VOLUME` | `sim-reminder-data` | 仅 Compose 使用的卷名 |

SQLite 数据目录权限 0700、文件权限 0600；启用 WAL、外键、5 秒 busy timeout、FULL synchronous。迁移按版本事务执行并保存 SHA-256；校验和改变或数据库版本高于程序时拒绝启动，不自动降级、删库或回滚。

## Telegram 命令

先在私聊发送 `/start` 绑定真实私聊地址。API 登录不能替代此步骤；无私聊地址的提醒会以 `no_private_chat` 永久失败。仅白名单用户可用。支持 `/cmd@当前Bot用户名`，发给其他 Bot 的命令忽略。

所有操作均通过按钮与持久化分步向导完成，不接受 query 字符串：

| 命令 | 行为 |
| --- | --- |
| `/start`, `/help` | 入口说明与当前时区 |
| `/service` | 服务菜单：服务列表 / 添加服务 / 提醒任务 / 设置 |
| `/service list` | 活跃服务分页列表；每条提供查看、编辑、归档按钮，翻页用按钮 |
| `/service add` | 分步创建：类别按钮 → 名称 →（SIM：卡类型/运营商/号码/地区/ICCID）→（generic：服务商/公开标识/管理链接/备注）→ 预览 → 确认 |
| `/service view <id>` | 服务详情：资料、有效期分页，以及查看、续期、编辑条款、规则、历史、编辑服务、归档/恢复、添加有效期按钮 |
| `/service edit <id>` | 分步编辑单个字段；空白表示清空（名称与卡类型除外），类别只能在同类 kind 内切换 |
| `/service archive <id>` | 显示归档影响与确认按钮；确认后归档，之后可在详情恢复 |
| `/service period add <service_id>` | 分步创建有效期：SIM 四种类型 / generic 仅 custom → 名称（可跳过）→ date 或 datetime → 截止 → 开始时间（可跳过）→ 预览 |
| `/service terms <period_id>` | 分步替换条款：模式按钮 → recurring 数量/单位 → 金额与币种（可跳过）→ 预览 |
| `/service rule add <period_id>` | 分步创建提醒规则：提前天数 → HH:MM → 启用/禁用 → 预览 |
| `/renew <period_id>` | 分步续期：截止精度 → 新截止 → 金额/币种（可跳过）→ 备注（可跳过）→ 预览 → 确认 |
| `/reminders` | 提醒任务分页；pending/failed 提供取消按钮，sent 不提供 |
| `/settings` | 当前 IANA 时区与私聊绑定；可进入修改时区 |
| `/cancel` | 取消当前未完成流程；不写入被取消草案中的修改 |

读取类命令（`/service list`、`/service view`、`/reminders`、`/settings`、`/help`）不会清除未完成流程。发起另一个写流程时，Bot 会给出“继续当前 / 取消并开始”按钮：只有点击“取消并开始”才丢弃草案。字段校验失败保留当前步骤与已填内容；每个可编辑步骤提供“上一步 / 跳过（仅可选字段）/ 取消”，最终提交前显示摘要与“确认 / 返回修改”。

### 对话、按钮与数据约束

- 每个用户最多一个未完成流程，持久化在 `bot_dialogs`，15 分钟 TTL，重启后仍可继续；重复点击同一目标不会延长 TTL。切换目标或动作必须先确认，`/cancel` 可随时丢弃。
- 流程动作与步骤由代码固定（`renew`、`service_add`、`service_edit`、`period_add`、`terms_edit`、`rule_add`、`rule_edit`、`timezone`）；用户不能提交任意动作名。草案仅存数据库，不写日志、回调或错误输出，上限 16 KiB。
- 按钮使用 `v2:` 前缀，payload 只含动作与十进制 ID。每次回调都重新校验私聊、发送者、白名单、内部用户与资源所有权，再读取当前对象；过期、已归档、已取消或版本不匹配时提示状态已变化且不写入。重复点击只读按钮返回最新详情；重复点击写入按钮依赖业务幂等或当前状态判定。
- 续期确认在同一事务内使用启动对话时的周期，写入快照、增加 cycle、取消旧任务并删除对话；Mini App 或其他请求已推进 cycle 时拒绝写入并保留草案。相同周期同参数重放返回既有历史。
- 创建有效期不会自动创建提醒规则。SIM `period_type` 为 `number_validity`、`plan_validity`、`data_package_validity`、`custom`；非 SIM 只能 `custom`。费用为非负整数最小货币单位（允许 0），与大写三字母币种同时提供；不换算、不代表付款。
- 修改时区时 date 精度按新时区重算当地日历日结束，datetime 绝对时刻不变；相关有效期推进 cycle 并重新排程。归档服务会保留有效期、续期历史与已发送记录，停止未完成提醒任务。

## HTTP API

默认只监听回环地址。公网部署必须通过 HTTPS 反向代理/Tunnel 并维持同源。无 CORS、Cookie、JWT、登录端点或鉴权绕过。除探针外每个请求带 `Authorization: tma <原始initData>`。使用 HMAC-SHA256 验签，拒绝重复字段、仅解码一次、原始数据上限 16 KiB；默认有效期 5 分钟，未来时钟容忍 30 秒。有效期内 initData 可重放。不得记录 Token、initData、授权头、手机号、ICCID、备注、请求体或包含凭据的 SDK URL。

### 实现路由

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/healthz`, `/readyz` | 无鉴权存活/就绪探针 |
| GET, PATCH | `/api/v1/me` | 当前用户；PATCH 时区 |
| GET, POST | `/api/v1/categories` | 列出可见内置/本人类别；创建本人 generic 类别 |
| PATCH | `/api/v1/categories/{id}` | 只接受 `{"name":"..."}`；仅本人自定义类别，内置/他人类别拒绝 |
| GET | `/api/v1/overview` | 旧 SIM-only 全量统计与主有效期分页；只接受 `limit,after_id` |
| GET | `/api/v1/services` | 服务列表；`q,category_id,status,limit,after_id` |
| POST | `/api/v1/services` | 创建服务 |
| GET, PATCH, DELETE | `/api/v1/services/{id}` | 读取/更新/归档，PATCH `status=active` 恢复 |
| POST | `/api/v1/services/{id}/delete` | 永久级联删除，无 body/query |
| GET, POST | `/api/v1/services/{id}/periods` | 列表/创建有效期 |
| GET | `/api/v1/services/overview` | 全筛选集 counts 与服务/主有效期分页 |
| GET, POST | `/api/v1/sims` | 旧 SIM 专用列表/创建 |
| GET, PATCH, DELETE | `/api/v1/sims/{id}` | 旧 SIM 读取/更新/归档 |
| POST | `/api/v1/sims/{id}/delete` | 永久删除 SIM 及级联数据，无 body/query |
| GET, POST | `/api/v1/sims/{id}/periods` | 旧 SIM 有效期列表/创建 |
| GET, PATCH, DELETE | `/api/v1/periods/{id}` | 读取/更新/取消；PATCH 要 `expected_cycle` |
| POST | `/api/v1/periods/{id}/delete?expected_cycle=N` | 永久删除有效期及关联记录，无 body |
| GET, POST | `/api/v1/periods/{id}/rules` | 列出/创建提醒规则 |
| GET, PATCH, DELETE | `/api/v1/rules/{id}` | 读取/更新/禁用；PATCH 要 `expected_revision` |
| POST | `/api/v1/rules/{id}/delete?expected_revision=N` | 永久删除规则/任务，无 body |
| GET, POST | `/api/v1/periods/{id}/renewals` | 续期历史/确认续期 |
| GET | `/api/v1/reminders` | 列任务，可选 status |
| GET | `/api/v1/reminders/{id}` | 提醒任务详情 |
| POST | `/api/v1/reminders/{id}/cancel` | 取消可取消任务 |

旧 `/api/v1/overview` 只返回 SIM，保留 `{sim,primary_period}`，仅接受 `limit,after_id`。新 `/api/v1/services/overview` 返回 `{service,primary_period}`。通用列表与 overview 只接受 `q,category_id,status,limit,after_id`；status 是 `active`（默认）、`due_soon`、`expired`、`no_period`、`archived`、`all`。搜索只查 name/provider/identifier。筛选在分页前应用；counts 覆盖全匹配结果，并与 items 使用同一读快照/当前时刻。主有效期为未取消的最早截止，同截止取最大 ID。临期按用户时区日历日 0–7 天计算。

资源 ID 是十进制字符串；cycle/revision/金额为整数。列表按 ID 升序 keyset 分页，limit 默认 20、范围 1–100；`after_id` 为非负十进制字符串。列表响应 `{"data":...,"next_cursor":...}`，单对象 `{"data":...}`，错误 `{"error":{"code":"...","message":"..."}}`。创建 201、读取/修改 200、归档/取消/删除 204；新续期 201，同参数重放 200。非法输入 400、未认证 401、白名单外 403、缺失/他人资源 404、版本冲突 409、超大请求 413、Content-Type 错误 415、不可用 503、内部错误 500。JSON 上限 64 KiB，必须 `application/json`，拒绝未知字段/尾随 JSON。查询串拒绝未知、重复、空键。

`DELETE` 服务/SIM 是归档，period DELETE 是取消，rule DELETE 是禁用；不可逆删除必须使用无 body 的 `POST .../delete`。period 删除须带正的当前 cycle，rule 删除须带正的当前 revision。跨用户资源返回 404；删除 ID 不复用。

归档服务不能新建有效期（409）；恢复为 active 后再创建。真实删除服务会级联删除其有效期、规则、任务、续期历史及续期对话；删除有效期保留所属服务与其他有效期。

### 请求与响应 JSON 字段

服务对象字段完整如下（非适用值显式为 null）：

```json
{"id":"12","user_id":"7","name":"Cloud host","category_id":"2","provider":"Example Host","identifier":"host.example","management_url":"https://example.invalid/account","notes":null,"status":"active","created_at":"2026-10-19T00:00:00Z","updated_at":"2026-10-19T00:00:00Z","sim_details":null}
```

创建服务必须提供 `name`、字符串 `category_id`。可选 nullable 字段为 `provider,identifier,management_url,notes,sim_details`。通用创建示例：

```json
{"name":"Cloud host","category_id":"2","provider":"Example Host","identifier":"host.example","management_url":"https://example.invalid/account","notes":null}
```

`sim_details` 对 generic 必须缺省/null；SIM 类别创建需包含对象及 `card_type`，其他子字段可 null：

```json
{"name":"Travel SIM","category_id":"1","provider":"Carrier","sim_details":{"card_type":"esim","phone_number":null,"country_or_region":"Japan","iccid":null}}
```

SIM detail 形状为 `card_type,phone_number,country_or_region,iccid`。PATCH 可传 `name,category_id,status` 及可清空的 `provider,identifier,management_url,notes,sim_details`。缺省保持不变；嵌套 sim_details 对象只更新提供字段，card_type 不可 null，其他子字段可 null 清空；SIM 上 `sim_details:null` 非法。嵌套未知字段拒绝。Bot 使用扁平 SIM 键；只有 HTTP 请求/响应使用嵌套对象。

服务 PATCH 示例（省略字段不变，`null` 清除可空字段）：

```json
{"name":"Renamed service","provider":null,"identifier":"host-02","status":"active"}
```

SIM 子对象 PATCH 只发送需要改的子字段，例如 `{"sim_details":{"phone_number":null}}`；对象内省略的字段保持不变。创建/更新全部字段在服务端同一事务中写入父项与 SIM 资料。

类别对象字段为 `id,user_id,code,name,kind,created_at,updated_at`，内置类别 `user_id` 为 null。创建请求 `{"name":"My category"}`；PATCH 同形，只可改本人自定义类别名称。他人/不存在类别返回 404，尝试改名内置类别或同用户重名返回 409。示例响应：

```json
{"id":"1","user_id":null,"code":"sim","name":"SIM 与 eSIM","kind":"sim","created_at":"2026-10-19T00:00:00Z","updated_at":"2026-10-19T00:00:00Z"}
```

period 创建必需 `period_type,date_precision,expires_at`；`name` 省略时为空字符串，`starts_at` 可 null，`terms` 可省略或 null。有效期响应字段完整为 `id,service_id,sim_id,period_type,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,terms,created_at,updated_at`。SIM 的 `sim_id` 是字符串，非 SIM 为 null。terms 响应总有 `mode,interval_count,interval_unit,amount,currency,revision`。

```json
{"period_type":"custom","name":"Monthly plan","starts_at":null,"date_precision":"date","expires_at":"2027-03-31","terms":{"mode":"recurring","interval_count":1,"interval_unit":"month","amount":1200,"currency":"USD"}}
```

可写 terms 对象**必须包含全部五个键**：`mode,interval_count,interval_unit,amount,currency`；不适用值传 JSON null。revision 仅响应，不可写。示例：一次性无费用 `{"mode":"once","interval_count":null,"interval_unit":null,"amount":null,"currency":null}`；custom 保留价格时 interval 两字段为 null，amount/currency 成对。recurring 要正整数数量与 `day|week|month|year` 单位；金额非负 int64，币种三位大写字母。

```json
{"id":"20","service_id":"12","sim_id":null,"period_type":"custom","name":"Monthly plan","starts_at":null,"expires_at":"2027-03-31T16:00:00Z","date_precision":"date","local_expires_date":"2027-03-31","timezone":"Asia/Taipei","status":"active","cycle":1,"terms":{"mode":"recurring","interval_count":1,"interval_unit":"month","amount":1200,"currency":"USD","revision":1},"created_at":"2026-10-19T00:00:00Z","updated_at":"2026-10-19T00:00:00Z"}
```

period PATCH 必带 `expected_cycle`；若包含 terms，还必带 `expected_terms_revision`。有 expected terms revision 而没有 terms 是非法输入。`terms:null` 重置为 custom，但不会重置 revision。陈旧版本即使 no-op 也冲突；版本匹配且条款无变化时不增加 revision。terms-only 只增加 terms revision，不改 cycle 或 job。续期不能自动采用约定费用。

通用 overview 形状：

```json
{"data":{"counts":{"total":0,"due_soon":0,"expired":0,"no_period":0},"items":[]},"next_cursor":null}
```

旧 SIM 创建 payload 保持 `{ "name":"Travel SIM", "card_type":"esim", "operator":"Carrier", "phone_number":null, "country_or_region":null, "iccid":null, "notes":null }`，响应字段仍为 `id,user_id,name,card_type,operator,phone_number,country_or_region,iccid,notes,status,created_at,updated_at`；旧 overview item 仍为 `{ "sim":<旧 SIM 对象>, "primary_period":<有效期对象或 null> }`。通用 service 用 `provider`。period 以 `service_id` 为规范父键，旧 `sim_id` 仅对 SIM 返回数字字符串，绝不伪造。精确模型不在 MINI_APP 文档重复。

旧 `/api/v1/overview?limit=20&after_id=0` 只接受 `limit,after_id`，未知参数（包括 `status`）返回 400。空响应为 `{"data":{"counts":{"total":0,"due_soon":0,"expired":0,"no_period":0},"items":[]},"next_cursor":null}`；有数据时每个 item 为 `{"sim":<与 /sims/{id} 相同的对象>,"primary_period":<与 /periods/{id} 相同或 null>}`。统计只包含当前用户 `active` SIM，包含无主有效期 SIM；归档 SIM 通过 `/sims` 分页获取。主有效期忽略 `cancelled`，取最早截止，同截止取最大 ID；已过期项目仍可成为主有效期。`expired` 比较 `expires_at <= now`；`due_soon` 是未过期且用户时区到期日距今天 0–7 个日历日（含今天）；`no_period` 是无任何未取消有效期的 SIM 数。每张 SIM 只归类一次，超过 7 天只计入 `total`；统计来自同一读快照/当前时刻，不随分页改变。

其他共用资源响应字段：规则 `id,validity_period_id,days_before,reminder_time,enabled,revision,created_at,updated_at`；续期 `id,validity_period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at`；提醒任务 `id,validity_period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,sent_at,telegram_message_id,last_error_code,created_at,updated_at`。规则创建接受 `days_before,reminder_time,enabled`（enabled 默认 true）；PATCH 要 `expected_revision`。续期请求必需 `expected_cycle,date_precision,expires_at`，可选 `amount,currency,notes`，金额/币种同时提供或省略。me 响应字段为 `id,telegram_user_id,timezone,has_private_chat,created_at,updated_at`，PATCH 接受 IANA `timezone`。

规则创建示例（`POST /api/v1/periods/{id}/rules`）：

```json
{"days_before":7,"reminder_time":"09:00","enabled":true}
```

规则 PATCH 示例（必须带当前 revision）：

```json
{"expected_revision":1,"days_before":3,"reminder_time":"08:30","enabled":false}
```

续期请求示例（`POST /api/v1/periods/{id}/renewals`）：

```json
{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-31","amount":1200,"currency":"USD","notes":"用户确认付款"}
```

续期新建返回 201；同一周期相同参数重放返回 200 和原记录。同周期但日期、精度、金额/币种或备注不同返回 409；陈旧 cycle 或已取消有效期也返回 409。新截止必须晚于旧截止与当前时间，否则 400。提醒任务列表仅接受 `status,limit,after_id`，status 为 `all`（默认）、`pending`、`sending`、`sent`、`failed`、`cancelled`；任务取消成功为 204，已发送返回 409，已取消仍为 204。`PATCH /me` 示例为 `{"timezone":"Asia/Taipei"}`。

## 时间、条款与提醒投递

- `date` 输入严格 `YYYY-MM-DD`，用户时区该日结束前有效，存为下一本地日界的排他截止。例如台北 2027-01-31 截止为 `2027-01-31T16:00:00Z`。`datetime` 为带 offset 的 RFC3339；Bot query 的 `+` 须编码。
- 提前天数按本地日历计算。DST 不存在的提醒时刻顺延至当天跳时后的首个有效分钟；重复时刻选择较早 UTC 候选；不存在的整日输入拒绝。提醒时刻必须早于截止。
- 修改时区时 date 保留当地日历日期并重算 UTC 截止；datetime 保留绝对时刻，仅重算本地展示/提醒。非取消周期递增，未发送任务取消，旧周期按钮失效，历史快照不变。
- 改截止/精度增加 cycle；规则内容变化增加 revision；terms revision 独立。请求带 expected 版本，terms-only 不触发周期任务变动。取消有效期不可恢复；归档后可恢复并重新排程。续期要求新截止晚于旧截止与当前时间；相同周期、相同参数重放原记录。
- 单扫描循环、单发送 worker。任务幂等键为 `(period,rule,cycle,rule_revision)`；成功发送按 `(period,rule,expires_at_snapshot)` 去重。因此只改提醒时刻、禁用再启用或归档恢复但截止相同，不会重复发送。
- 默认补发窗口 24 小时且仅补发未过期任务。同一有效期当前周期候选只保留最近一条，其余标为 `superseded_catchup`；窗口外为 `missed_window`，过期为 `expired`。未来任务不合并。取消、永久失败、已发送任务不会因扫描复活。
- 网络/超时/5xx/未分类错误有限重试：30 秒、1 分钟、2 分钟、4 分钟、8 分钟，最多 6 次尝试。429 至少等待 Telegram `retry_after`，且不得早于本次退避。明确 400/401/403/404/409、迁移错误、无私聊地址为永久失败。重试受补发窗口、截止、当前周期及规则状态限制。
- 发送租约 60 秒；过期租约可恢复。发送最长 10 秒，结果写回使用独立最长 5 秒 context。取消与在途发送竞态时，已成功投递记录 sent；失败则保留 cancelled 且不重试。
- **不保证 exactly-once。** Telegram 已收消息而本地尚未写入 sent 时崩溃可能导致重复；补发窗口和次数上限也可能漏发。取消不能撤回已发消息。
- 修改白名单需重启；撤权取消未完成任务（`unauthorized`），在途成功如实记录；重新加入名单不会复活已取消任务。

## Migration 003、升级与回退

`003_service_items.sql` 将旧 SIM 父表推广为 `service_items`，SIM 字段拆入 `sim_details`，增加类别及可选 `period_terms`，有效期父关联改为 `service_id`。必须保留 IDs、所有者、日期、cycle/revision、规则、续期快照、任务状态/租约/token、对话及 AUTOINCREMENT 高水位。旧有效期无 terms 行时读取为 custom revision 1。迁移按版本事务执行，任一步失败必须回滚；旧的 `001`/`002` 文件不得修改。

迁移内嵌于二进制，服务启动时在版本事务中自动应用，没有单独的手工 migrate 命令。升级顺序为：停写并备份 → 在可丢弃副本启动新版本并检查 → 确认后用新版本启动正式卷并验收。新版本首次启动会应用 003；成功后旧版本不得再连接该卷。

本次升级在可丢弃副本上分别验证了实际 v1 空库、v2 混合数据：v2 基线为 4 SIM、3 有效期、1 续期，sequence 保留；两份副本各升级一次并通过 API smoke，源数据库 hash 未变化。这不是生产迁移授权或生产库验收。

**迁移后旧二进制不能原地运行。** 迁移器拒绝更高 schema，003 也改变表结构。回退只能停写后把升级前完整卷备份恢复到一个新空卷，并用旧二进制运行恢复副本。升级后的卷必须保留。升级后若有写入，停写并另存新卷备份；先决定如何保留新增数据再恢复。不得覆盖旧卷/升级卷、删表、清库或重置 sequence 来模拟降级。

正式升级前，停掉所有写入者/调度器，制作并验证包含 DB/WAL/SHM 的完整卷备份，在副本运行迁移并检查。缺少备份、未确认单实例停写或 integrity/migration 检查失败时，禁止升级正式卷。这是明确维护操作，不随发布自动进行。

### 停机卷备份

备份含敏感数据，必须限制访问并加密。不能在运行中只复制 `.db`；采用停机完整卷快照。下例使用默认卷名，归档路径不得覆盖已有文件：

```sh
export REMIND_DATA_VOLUME=sim-reminder-data
mkdir -p backups
chmod 700 backups
docker compose stop app
test -z "$(docker compose ps --status running -q app)"
docker run --rm --network none \
  -v "$REMIND_DATA_VOLUME:/data:ro" \
  -v "$PWD/backups:/backup" \
  alpine:3.22 sh -c 'umask 077; test ! -e /backup/remind-data.tar.gz && tar -C /data -czf /backup/remind-data.tar.gz .'
docker compose up -d
curl -fsS http://127.0.0.1:8080/readyz
```

每条命令都检查退出码；归档失败不能视作备份成功。辅助镜像离线运维前须预先拉取。记录应用版本、卷名、备份时间，并定期演练恢复。

### 恢复到新卷

使用相同应用版本或明确支持备份 schema 的版本。目的卷必须全新空卷；先确认卷名不存在，不能清空旧卷复用。

```sh
docker compose stop app
test -z "$(docker compose ps --status running -q app)"
if docker volume inspect sim-reminder-restored >/dev/null 2>&1; then
  echo '目的卷已存在，请另选一个新卷名，不得清空复用' >&2
  exit 1
fi
docker volume create sim-reminder-restored
docker run --rm --network none \
  -v sim-reminder-restored:/data \
  -v "$PWD/backups:/backup:ro" \
  alpine:3.22 sh -ec 'test -z "$(find /data -mindepth 1 -maxdepth 1 -print -quit)"; tar -C /data -xzf /backup/remind-data.tar.gz; chown -R 65532:65532 /data; chmod 0700 /data'
export REMIND_DATA_VOLUME=sim-reminder-restored
docker compose up -d --force-recreate
curl -fsS http://127.0.0.1:8080/readyz
```

把新卷名持久写入部署配置；旧卷停止保留。不要运行 `docker compose down -v`。ready 不代表恢复验收：通过 Bot/API 比较服务、有效期、规则、续期及 sent 状态；重启确认已完成任务不重复发送。恢复后的 pending 任务可能按补发窗口继续投递。

## Docker 部署

Compose 使用 Go `app`、非 root nginx `web`、挂载 `/data` 的命名卷与同源代理。不能让多个 app 实例共用数据库，不直接公开 Go API。保护 `.env` 和展开凭据的 `docker compose config` 输出。镜像 tag 不等于不可变 digest，发布需记录实际 digest。

- `web` 是 Node 24 构建、nginx unprivileged 运行的独立静态/反向代理容器；构建产物位于 `/app/`，`/api/v1/`、`/healthz`、`/readyz` 同源转发到 `app`，Go 不托管静态资源。
- `MINIAPP_HTTP_PORT` 仅控制宿主回环端口（默认 `8081`）；外部 HTTPS 代理/Tunnel 转发整个站点到该端口，不直接公开 Go `8080`。
- `web` 不挂载数据卷、不接收 Token；Token、白名单和数据库只注入 `app`。CSP 哈希由构建脚本生成，不使用 `unsafe-inline`。
- `app` 固定 `golang:1.27.1-bookworm` 构建、`gcr.io/distroless/static-debian12:nonroot` 运行；`web` 固定 Node `24.22.0-alpine3.23` 与 `nginxinc/nginx-unprivileged:1.28.0-alpine`，不使用 `latest`。
- 容器 uid/gid 为 **65532:65532**，`/data` 为 0700；bind mount 须先在宿主设置相同所有权与权限。权限错误应失败，不切回 root。
- 默认命名卷 `sim-reminder-data` 挂载整个 `/data`（含 DB/WAL/SHM）；不能只挂单个 `.db`，禁止多实例共用同一数据库。
- 宿主只发布 `127.0.0.1:8080:8080` 与 `127.0.0.1:${MINIAPP_HTTP_PORT:-8081}:8080`；公网入口由 HTTPS 代理提供。
- `restart: unless-stopped`；app 停止宽限 40 秒、web 10 秒。SIGTERM 先撤销 ready、停止新工作与任务领取，最多等待 30 秒完成在途工作和结果写回，再关闭数据库。

```sh
docker compose build
docker compose up -d
curl -fsS http://127.0.0.1:8080/readyz
curl -fsSI http://127.0.0.1:${MINIAPP_HTTP_PORT:-8081}/app/
docker compose logs --tail=100 app web
```

恢复验收不能只看 ready：通过 Bot/API 比较服务、有效期、规则、续期历史数量与内容；确认 sent 记录仍在，pending/failed/cancelled 状态正确，重启不重复发送正常完成任务。过期租约和补发遵循既有窗口规则；恢复可能触发有效窗口内投递，应提前告知用户。建议先记录基线，再演练 restart、备份与恢复。

本次未运行 Docker Compose build，因为环境没有 Docker；不得将上述命令写成已验证。

## 验证记录与限制

本次服务/订阅升级工作实际验证：

- `GOTOOLCHAIN=local go test ./internal/storage ./internal/service ./internal/reminder ./internal/bot ./internal/api` 通过；随后完整 Go 测试套件通过。
- `GOTOOLCHAIN=local go test -race ./...` 通过；`CGO_ENABLED=0 GOTOOLCHAIN=local go build -o /tmp/remind-smoke .` 通过。
- v1 空库与 v2 混合数据 disposable copies 各升级一次并通过 API smoke；源库 hash 未变。v2 基线 4 SIM、3 period、1 renewal，sequence 保留。`sim_details` 与 `period_terms` 使用 `WITHOUT ROWID`。
- 完成目录式完整备份 → 恢复到空目录 → 升级至 003 → API 检查与重启后历史/sent 完整性检查。Docker 命名卷流程未执行。
- 旧二进制不能读取迁移 003；回退需完整备份恢复到新空卷。
- 前端 180 tests 通过，`svelte-check` 零错误零警告，生产构建完成。
- 浏览器实际走查服务和旧 SIM 流程、冲突处理、响应宽度/主题、旧深链和通用 seed 数据。
- 浏览器实际验证“重置约定条款”：持久化 int64 最大值费用重置为 custom/null、terms revision 2，period cycle 保持 1。分页实测 22 项：计数在翻页间稳定，第二页包含名称为“分页验收 22”的条目。
- Docker Compose build 未运行：环境没有 Docker 可执行文件且没有可用 Docker socket。真实 Telegram 投递、iOS/Android 原生 WebView/返回行为及外部 HTTPS 未验证。
- Bot 命令交互改造：迁移 004、通用对话草案、v2 按钮与分步向导在 `internal/storage`、`internal/service`、`internal/bot` 的 SQLite + Telegram HTTP stub 上验证（服务/有效期/条款/规则/时区向导、只读命令不打断、替换确认、v1 回调兼容、重复确认与陈旧周期拒绝）。未在真实 Telegram 客户端或移动端验证按钮排版与投递。

以下是需要具备环境时执行的检查，不代表已运行：

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go build -o /tmp/remind-smoke .
docker compose build
```

race 需要合适 C 工具链。真实投递需要测试 Token、本人白名单和私聊；原生移动端与公网 HTTPS 验收需要外部设备及部署权限。Docker 命名卷备份/恢复演练同样未完成。

## Changelog

### Unreleased

- 将 SIM 专用管理推广为通用 service item，加入内置/自定义类别、SIM 专属资料、通用 API/Bot 与旧 SIM 兼容。
- 增加独立版本化的描述性有效期条款；条款不自动扣款/推算到期，terms-only 编辑不使提醒任务失效。
- 增加服务端全量筛选/搜索/类别 overview、通用 Mini App 路由与表单，以及旧 SIM 深链跳转。
- 加入迁移 003 与完整卷备份、禁止原地二进制回退、恢复到新卷的操作说明。
- Bot 增加持久化多步对话（迁移 004）、Inline Keyboard 浏览/归档确认/任务取消、`/settings`，续期改为输入—预览—确认；旧命令与 v1 回调保持兼容。
- 更新 API/Bot、安全、迁移、Mini App 和验证记录。Docker build、真实 Telegram、iOS/Android 原生行为及外部 HTTPS 按实际情况标为未验证。

## Git 提交规范

提交说明格式为 `type: concise English description`，使用简短清晰的英文动词短语；类型包括 `feat,fix,refactor,docs,test,style,build,ci,perf,chore`。每次只提交逻辑相关内容，不提交密钥、用户数据或临时文件。提交前检查 diff 并运行相关测试。不得未经明确授权自动 commit、amend 或改写历史。
