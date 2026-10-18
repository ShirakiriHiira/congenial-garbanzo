# SIM / eSIM 有效期提醒

单实例 Go 服务：Telegram 私聊管理 SIM/eSIM、多有效期、多提醒规则、明确确认续期；SQLite 保存业务数据、续期历史、提醒任务和续期对话。提供 HTTP API 与 Svelte Mini App 前端，使用 Telegram `initData` 验签。稍后提醒、运营商自动续费和支付不在当前范围。

## 本地运行

要求 Go **1.27.1**；SQLite 使用纯 Go `modernc.org/sqlite v1.60.1`，生产构建不需要 CGO。迁移与 IANA 时区数据嵌入二进制。

```sh
cp .env.example .env
chmod 600 .env
# 编辑 .env，填写 Token；不要提交或分享它。
export ALLOWED_TELEGRAM_USER_IDS='10001,10002'
CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -o ./bin/remind .
./bin/remind
```

`10001,10002` 只是格式示例，必须换成实际允许的 Telegram 用户 ID。白名单不能为空，不提供开放注册模式。

**本地 Token 优先级：当前工作目录 `.env` 的非空 `TELEGRAM_BOT_TOKEN` 优先于进程环境。** 文件缺失、读取失败或该键为空时才使用环境变量。其余配置只读进程环境，单纯写在 `.env` 中不会让本地二进制加载；需要 `export`。不要把不可信 `.env` 当 shell 脚本执行。生产容器仅通过环境传配置，不复制或挂载 `.env`。

启动失败时进程以 1 退出，`error` 字段给出具体原因：

| `error` 内容 | 处理 |
| --- | --- |
| `invalid configuration: TELEGRAM_BOT_TOKEN is required` | `.env` 中 Token 为空且进程环境未设置该变量 |
| `invalid configuration: ALLOWED_TELEGRAM_USER_IDS ...` | 未 `export` 白名单，或含非正整数/空项 |
| `invalid configuration: DATABASE_PATH/APP_TIMEZONE/...` | 对应环境变量不合法；这些变量不读 `.env` |
| `telegram initialization failed: ...` | Token 被 Telegram 拒绝，或本机无法访问 `api.telegram.org`（代理、DNS、TLS） |
| `telegram identity unavailable: ...` | 同上，`GetMe` 未返回有效的 Bot 用户名 |
| `storage unavailable: ...` / `migration failed: ...` | 数据库路径不可写或迁移校验失败 |
| `http listener unavailable: ...` | `HTTP_ADDR` 端口被占用或地址非法 |

```sh
curl -fsS http://127.0.0.1:8080/readyz
./bin/remind healthcheck
```

`healthcheck` 仅使用 `HTTP_ADDR` 请求本机 `/readyz`，2 秒超时；不加载 Token、不打开数据库，非 200 时非零退出。通配监听地址转换为回环地址。

Go **只**读 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` 环境变量，不读桌面环境的系统代理设置；需要走本机代理时显式设置 `TELEGRAM_API_PROXY`，例如 `export TELEGRAM_API_PROXY=http://127.0.0.1:7890`（Clash/Mihomo 的 socks 端口用 `socks5://127.0.0.1:7891`）。该代理只作用于 Telegram API，`healthcheck` 与本机 `/readyz` 不受影响。

## 本地调试 Mini App

正式 Mini App 的 `initData` 由 Telegram 客户端注入；普通浏览器打开 `http://127.0.0.1:5173/app/` 只会显示「请从 Telegram 内打开」。不提供生产鉴权绕过；无需 Telegram 的 UI/代码调试使用下述独立测试模式。

Telegram 官方仅在**测试环境**允许 HTTP 链接（[Mini Apps 文档](https://core.telegram.org/bots/webapps#using-bots-in-the-test-environment)），且测试环境需要独立账号和 Bot；本服务的 Bot 轮询未接入测试 API 基址。因此用现有 Bot 调试 Mini App 时，必须给本机一个 HTTPS 入口（`cloudflared` 快速隧道即可），不要改 `WEBAPP_AUTH_MAX_AGE` 来掩盖过期问题。

### 无 Telegram 的隔离测试模式

```sh
./scripts/test-miniapp.sh
# 浏览器打开 http://127.0.0.1:5180/app/
```

要求 Go 1.27.1、Node 24 和 npm。脚本优先使用 `MINIAPP_NODE_BIN`，其次系统 Node 24，再尝试 `.cache/` 中的 Node 24.22.0；每次执行 `npm ci` 并构建独立 `cmd/miniapp-test`。

- UI 固定监听 `127.0.0.1:5180`，API 固定监听 `127.0.0.1:8088`；不加载生产 `.env`、Token 或数据库配置。
- 每次启动创建临时 SQLite，预置 SIM/eSIM、临期、过期、归档和未命名有效期；真实 API/业务逻辑支持新增、编辑、归档、续期和删除，不是静态演示数据。
- 页面持续显示「本地测试模式」；同一次运行内修改保留，Ctrl-C 停止并清理数据库，重新启动恢复初始测试数据。测试模式不启动 Bot 或发送器，不发送 Telegram 提醒。
- 仅开发构建且 `VITE_MINIAPP_TEST_MODE=true` 时读取专用 `/__test/init-data`。测试进程用临时密钥签发测试身份，仍经过正常验签；正式进程没有此入口，生产构建不启用该模式。不要将测试端口通过隧道公开。
- 浏览器测试模式使用浏览器返回；正式 Mini App 使用 Telegram 原生返回按钮，页面不重复放置返回按钮。

### 1. 一条命令

已有 Cloudflare 自定义域名时，把隧道 origin 配成 `http://127.0.0.1:5173`，然后：

```sh
MINIAPP_PUBLIC_URL='https://sim.example.com' \
TELEGRAM_BOT_TOKEN='123:abc' \
ALLOWED_TELEGRAM_USER_IDS='你的Telegram用户ID' \
./scripts/dev-miniapp.sh
```

没有自定义域名时省略 `MINIAPP_PUBLIC_URL`，脚本会自动开 `cloudflared` 快速隧道。

脚本会依次完成：

- 检查 Node 24；系统没有时下载官方 Node 24 到 `.cache/` 并校验 SHA-256。
- `npm ci`、`go build`，启动 Go API（`127.0.0.1:8080`）和 Vite（`127.0.0.1:5173`，`/api` 反代到 Go）。
- 设置了 `MINIAPP_PUBLIC_URL` 时直接使用该 HTTPS URL；否则启动 `cloudflared` 快速隧道并解析出 URL。
- 调 Bot API `setChatMenuButton`，只给白名单里的第一个用户设置菜单按钮，URL 指向 `<公网地址>/app/`。
- 打印 URL、数据库路径和日志目录；Ctrl-C 会停止全部进程。

`MINIAPP_PUBLIC_URL` 必须是纯 origin（`https://host`，可带 `/app`），不能带其他路径或查询串；Cloudflare 控制台的 Public Hostname 应转到 `http://127.0.0.1:5173`。Vite 只额外允许这个精确 Host。

默认使用 `./data/miniapp-test.db`，不会写现有数据。要测试已有卡片时显式覆盖，并确保没有其他实例正在使用该库：

```sh
DATABASE_PATH=./data/remind.db TELEGRAM_BOT_TOKEN='...' ALLOWED_TELEGRAM_USER_IDS='...' ./scripts/dev-miniapp.sh
```

Token 不会由脚本打印；但直接写在命令行会进入 shell 历史，介意时改用 `.env`（脚本也支持只在该文件里放 `TELEGRAM_BOT_TOKEN`）。

### 2. 手动排错

如果自动设置菜单按钮失败，脚本仍会打印 HTTPS URL；在 BotFather 执行 `/setmenubutton`，填入：

```text
https://<你的域名或随机隧道>/app/
```

也可以手动只跑隧道（不使用 `MINIAPP_PUBLIC_URL` 时）：

```sh
cloudflared tunnel --url http://127.0.0.1:5173
```

隧道必须指向 **5173**，不能指向 8080，否则 `/app/` 没有静态资源。`*.trycloudflare.com` 与 `MINIAPP_PUBLIC_URL` 的 Host 已加入 Vite 的 `allowedHosts`。

### 3. 必查项

- 首次使用先私聊 `/start`：Mini App 鉴权只创建/识别用户，不绑定私聊地址；`has_private_chat=false` 时提醒会失败。
- 非白名单账号得到 403；这是 `ALLOWED_TELEGRAM_USER_IDS` 未包含该 ID，不是前端缺陷。
- 页面显示「请从 Telegram 内打开」说明不是从 Telegram 打开；显示「会话已过期」说明 `initData` 超过默认 5 分钟，关闭并重新打开 Mini App。
- 所有 API 都是同源 `/api/v1`，没有 CORS；隧道只暴露 5173 即可。
- 调试时会真实读写数据库并启动提醒调度；不要用生产数据卷，也不要同时运行两个使用同一 Bot Token 的轮询实例。


### 4. 真机 WebView 调试

按 [Telegram 官方调试模式](https://core.telegram.org/bots/webapps#debug-mode-for-mini-apps)：iOS 连点 Settings 图标 10 次开启 *Allow Web View Inspection*，在 Mac Safari 的 *Develop* 菜单选设备；Android 在 Telegram 设置里长按版本号开启 *WebView Debug*，再用 `chrome://inspect/#devices`；桌面端用 Beta 版开启 *Enable webview inspection* 后右键 *Inspect*。


### 配置

| 环境变量 | 默认值 | 约束/作用 |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | 无 | 必填；仅本地加载器保留上述 `.env` 优先行为 |
| `ALLOWED_TELEGRAM_USER_IDS` | 无 | 必填，逗号分隔正 int64；去重；空值、非法值拒绝启动 |
| `DATABASE_PATH` | `./data/remind.db` | 文件型 SQLite；不能使用内存库启动正式服务 |
| `APP_TIMEZONE` | `Asia/Taipei` | 合法 IANA 时区；只作为新用户初始时区，不覆盖已有设置 |
| `REMINDER_SCAN_INTERVAL` | `30s` | Go duration，至少 `1s` |
| `REMINDER_CATCHUP_WINDOW` | `24h` | 正 duration，限制离线补发窗口 |
| `WEBAPP_AUTH_MAX_AGE` | `5m` | 正 duration，initData 最大年龄 |
| `HTTP_ADDR` | `127.0.0.1:8080` | 容器固定为 `:8080` |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `TELEGRAM_API_PROXY` | 无 | 可选。Telegram API 出口代理，`http`/`https`/`socks5`/`socks5h`，支持 `user:pass@`；裸 `host:port` 按 `http` 处理。空值仍读 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` |
| `REMIND_DATA_VOLUME` | `sim-reminder-data` | 仅 Compose 使用的卷名，不是应用配置 |

配置、迁移、SDK 初始化或 HTTP 监听失败会阻止完整启动。数据库父目录权限 0700、数据库文件权限 0600；WAL、外键、5 秒 busy timeout、FULL synchronous。迁移按版本事务执行并记录 SHA-256；同版本校验和改变或数据库版本高于程序时拒绝启动，不自动降级、删库或回滚。

## Telegram 使用

先向 Bot 私聊 `/start`，绑定真实私聊接收地址。API 登录不能替代此步骤：没有私聊地址的提醒会永久失败，原因 `no_private_chat`。只有白名单用户可用；群聊不提供业务数据。支持 `/cmd@当前Bot用户名`，忽略发给其他 Bot 的命令。

命令末尾查询串采用 URL 编码：空格写 `+`，字面 `+` 写 `%2B`，`&` 写 `%26`；中文可直接写。重复键、未知键、缺参数、非法转义均拒绝。不使用 shell 引号解析参数。

| 命令 | 格式与作用 |
| --- | --- |
| `/start`, `/help` | 介绍、全部命令、当前时区 |
| `/add` | `/add name=日本卡&card_type=esim`；`card_type` 为 `physical_sim` 或 `esim`；可选 `operator,phone_number,country_or_region,iccid,notes` |
| `/list` | `/list [after_id]`；每页 10 张卡，附下一页命令 |
| `/view` | `/view sim <id>`；`/view period <id>` |
| `/view` 列表 | `/view sim <id> periods [after_id]`；`/view period <id> rules [after_id]`；`/view period <id> renewals [after_id]`，每页 10 条 |
| `/edit` 卡片 | `/edit sim <id> <query>`；包括 `status=active` 恢复归档卡 |
| `/edit` 有效期 | `/edit period new <sim_id> <query>`；`/edit period <id> expected_cycle=1&<query>` |
| `/edit` 规则 | `/edit rule new <period_id> days_before=1&reminder_time=09:00`；`/edit rule <id> expected_revision=1&<query>` |
| `/edit` 时区 | `/edit timezone Asia/Taipei` |
| `/delete` | `/delete sim <id>` 归档；`/delete period <id> <expected_cycle>` 取消有效期 |
| `/reminders` | `/reminders [pending\|sending\|sent\|failed\|cancelled\|all] [after_id]`；默认 all，每页 10 条 |
| `/renew` | `/renew <period_id> expected_cycle=1&date_precision=date&expires_at=2027-03-31`；可选金额/币种/备注 |
| `/cancel` | 取消当前续期对话；`/cancel job <id>` 取消一次任务；`/cancel rule <id> <expected_revision>` 禁用规则 |

示例流程（把示例 ID 换成各次返回值）：

```text
/start
/add name=日本旅行卡&card_type=esim&operator=示例运营商
/edit period new 1 period_type=plan_validity&name=套餐有效期&date_precision=date&expires_at=2027-01-31
/edit rule new 1 days_before=30&reminder_time=09:00
/edit rule new 1 days_before=7&reminder_time=09:00
/edit rule new 1 days_before=3&reminder_time=09:00
/edit rule new 1 days_before=1&reminder_time=09:00
/edit rule new 1 days_before=0&reminder_time=09:00
/view period 1
/renew 1 expected_cycle=1&date_precision=date&expires_at=2027-03-31&amount=1000&currency=TWD&notes=已确认付款
/reminders all
```

`period_type` 支持 `number_validity`、`plan_validity`、`data_package_validity`、`custom`。创建有效期**不会自动创建任何提醒规则**；详情显示“未配置提醒”时必须显式新增规则。`amount` 是币种的最小货币单位，非负整数（允许 0），与大写三字母 `currency` 同时提供或同时省略；本服务不负责付款或货币单位换算。

提醒提供“已续期”“查看详情”按钮。“已续期”只打开 15 分钟持久化对话，不自动更改到期日；回复例如：

```text
date_precision=date&expires_at=2027-03-31
```

也可附 `amount,currency,notes`，不能附 `period_id` 或 `expected_cycle`，目标与周期来自已验证的对话。重启后未过期对话可继续；重复按钮不会延长到期时间。新有效业务命令清除旧对话，`/cancel` 可显式取消。旧周期按钮失效，必须查看当前有效期。所有数据、按钮和续期操作均按所有权检查。

## 时间、版本与投递语义

- `date` 输入严格 `YYYY-MM-DD`，在用户 IANA 时区当天结束前有效，存储次日本地日界这一排他截止。例如台北 `2027-01-31` 截止为 `2027-01-31T16:00:00Z`。`datetime` 必须是带 offset 的 RFC3339，例如 `2027-01-31T18:00:00%2B08:00`（Bot 查询串中的 `+` 要编码）。
- 提前天数按本地日历计算，不是减去 24 小时的倍数。DST 不存在的提醒时刻顺延到当天跳时后的第一个有效分钟；重复时刻选较早 UTC 候选。不存在的整日输入拒绝。提醒时刻必须严格早于截止。
- 改用户时区时，date 保留日历日期并重算 UTC 截止；datetime 保持绝对截止，只重算本地提醒。非取消项目周期增加、旧任务取消、旧按钮失效，历史快照不变。
- 改截止/精度增加 `cycle`；改规则增加 `revision`。更新必须带 `expected_cycle` / `expected_revision`，避免陈旧操作覆盖新设置。有效期取消后不能恢复；卡片归档后可以恢复，相关非取消项目重新排程。
- 续期必须明确新日期，新截止晚于旧截止与当前时间；过期项目续期后恢复 active。相同周期、相同参数重放返回原历史；同周期不同参数或陈旧版本返回冲突。已取消项目拒绝新续期。
- 单个扫描循环、单个发送工作者。任务按项目/规则/周期/规则版本幂等创建；成功发送按同规则、同截止去重，所以只改提醒时刻、禁用再启用或归档恢复而截止未变，不重复发送。
- 默认只补发最近 24 小时且尚未到期的任务；同有效期当前周期的到期候选只保留最近一条，其余取消为 `superseded_catchup`。窗口外为 `missed_window`，过期为 `expired`；未来任务不参与合并。取消/永久失败/已发送不会因重扫复活。
- 网络、超时、5xx 或未分类错误有限重试：30 秒、1 分钟、2 分钟、4 分钟、8 分钟，最多 6 次尝试。429 至少等待 Telegram `retry_after`，且不早于本次退避。明确的 400/401/403/404/409、迁移错误、无私聊地址为永久失败。重试仍受补发窗口、到期、当前周期和规则状态限制。
- 发送租约 60 秒，过期后可恢复尝试；发送最多 10 秒，结果使用独立最长 5 秒 context 写回。取消或修改与在途发送存在竞态：已发生的成功投递仍记录 sent，失败保持 cancelled 且不重试。
- **不承诺 exactly-once。** Telegram 与 SQLite 没有共同事务，“Telegram 已收但本地未记 sent”的崩溃窗口可能重发；补发窗口和次数上限也可能漏发。取消不能撤回已发送消息。
- 修改白名单需重启，撤权取消未完成任务（`unauthorized`），在途成功仍如实记录；重新加入名单不会复活已取消任务。

## HTTP API

默认只监听回环地址；部署到公网必须通过现有 HTTPS 反向代理或 Tunnel。默认不启用跨源 CORS，不提供登录、Cookie、JWT 或鉴权绕过。

除探针外，每请求带 `Authorization: tma <原始initData>`。`initData` 必须从 Telegram 实际启动的 Mini App 获得，本仓库第一期没有托管 UI；API 面向接入方，不接受手填 user_id、`initDataUnsafe` 或 URL query 凭据。标准 HMAC-SHA256 验签，单次 URL 解码，拒绝重复字段，原始数据最多 16 KiB；默认有效期 5 分钟，未来时钟容忍 30 秒。过期后重新进入 Mini App 获取新凭据。有效期内凭据可重放，不承诺一次性使用。不要将 Token、initData、授权头写入日志、截图或共享终端记录；关闭 shell tracing，并保护反向代理日志。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/healthz` | 无需认证；进程存活 `200 {"status":"ok"}` |
| GET | `/readyz` | 无需认证；启动完成且 1 秒数据库 Ping 成功为 200，否则 503 |
| GET, PATCH | `/api/v1/me` | 读取当前用户；PATCH `{ "timezone": "Asia/Taipei" }` |
| GET | `/api/v1/overview` | 全量统计 + 带主有效期的活跃卡片分页；只接受 `limit,after_id` |
| GET, POST | `/api/v1/sims` | 列表；创建卡片 |
| GET, PATCH, DELETE | `/api/v1/sims/{id}` | 详情、部分更新、归档；PATCH `status=active` 恢复 |
| POST | `/api/v1/sims/{id}/delete` | 永久删除卡片及关联有效期、规则、任务、续期历史与对话；无请求体，204 |
| GET, POST | `/api/v1/sims/{id}/periods` | 列表；创建有效期 |
| GET, PATCH, DELETE | `/api/v1/periods/{id}` | 详情；PATCH 带 `expected_cycle`；DELETE query 带 `expected_cycle` |
| POST | `/api/v1/periods/{id}/delete?expected_cycle=N` | 永久删除有效期及关联数据；无请求体，204；周期冲突 409 |
| GET, POST | `/api/v1/periods/{id}/rules` | 列表；创建规则 `days_before,reminder_time,enabled`（enabled 默认 true） |
| GET, PATCH, DELETE | `/api/v1/rules/{id}` | 详情；PATCH 带 `expected_revision`；DELETE query 带 `expected_revision`，软禁用 |
| POST | `/api/v1/rules/{id}/delete?expected_revision=N` | 永久删除规则及其提醒任务；无请求体，204；版本冲突 409 |
| GET, POST | `/api/v1/periods/{id}/renewals` | 历史；确认续期 |
| GET | `/api/v1/reminders` | 按可选 `status` 过滤任务 |
| GET | `/api/v1/reminders/{id}` | 任务详情 |
| POST | `/api/v1/reminders/{id}/cancel` | 取消一次任务；已发送为 409，已取消仍为 204 |

请求样例：

```json
{"name":"日本旅行卡","card_type":"esim","operator":"示例运营商"}
```

创建有效期（POST `/api/v1/sims/1/periods`）：

```json
{"period_type":"plan_validity","name":"套餐","date_precision":"date","expires_at":"2027-01-31"}
```

创建规则（POST `/api/v1/periods/1/rules`）：

```json
{"days_before":1,"reminder_time":"09:00","enabled":true}
```

确认续期（POST `/api/v1/periods/1/renewals`）：

```json
{"expected_cycle":1,"date_precision":"date","expires_at":"2027-03-31","amount":1000,"currency":"TWD"}
```

SIM 可选字段与 Bot `/add` 相同；有效期可选 `starts_at` 为 RFC3339 且必须早于截止。用户身份/时区来自认证后的用户记录，不从请求体接受他人的 ID 或时区。卡片 name 去首尾空白后必须非空；有效期 name 可省略或为空，持久化为 `""`，前端显示类型作为回退。name/operator 最多 200 Unicode 字符，notes 最多 4000，phone_number/country_or_region/iccid 最多 200。有效期精确时间输入支持秒，省略秒时补 `00`；日期精度与提醒规则的 `HH:mm` 不变。

响应中所有 ID 为十进制**字符串**；cycle/revision 为整数。`expires_at` 始终为 UTC RFC3339，另返回 `local_expires_date`、`date_precision`、`timezone`。列表 `limit` 默认 20，范围 1–100；`after_id` 为非负十进制字符串，按 ID 升序 keyset 分页：

```json
{"data":[],"next_cursor":null}
```

单对象 `{"data":{...}}`；错误 `{"error":{"code":"...","message":"..."}}`。创建 201，读取/修改 200，归档/取消/永久删除 204（无响应体），续期新建 201/同参数重放 200。畸形输入 400、无效凭据 401、名单外 403、无对象/跨用户 404、版本冲突 409、请求过大 413、错误 JSON Content-Type 415、暂不可用 503、其他错误 500。内部 SQL/SDK 错误不会直接输出。

JSON 请求必须使用 `Content-Type: application/json`，最大 64 KiB，拒绝未知字段和多段 JSON。PATCH 缺字段表示不变，`null` 仅可清除可空字段；有效期名称用 `"name":""` 清空，卡片名称、类型与状态不能清空。

永久删除不可撤销，Mini App 二次确认会说明关联数据范围；删除有效期不影响卡片与其他有效期。原 DELETE 接口与 Bot 命令继续归档/取消，不改变语义。迁移 `002_hard_delete.sql` 保留现有数据和 ID，增加关联级联删除与自增 ID，删除后不复用旧 ID；已进入外部发送请求的 Telegram 消息无法撤回。

### Mini App 总览

`GET /api/v1/overview?limit=20&after_id=0` 复用上述逐请求鉴权。顶层 `next_cursor` 是卡片 ID 字符串或 `null`；`data.items` 仅包含当前用户的 `active` 卡片，按卡片 ID 升序分页，**统计不随分页缩小**。

空用户响应：

```json
{"data":{"counts":{"total":0,"due_soon":0,"expired":0,"no_period":0},"items":[]},"next_cursor":null}
```

每个 `items` 元素为 `{ "sim": <卡片对象>, "primary_period": <有效期对象或 null> }`。两个对象沿用现有详情接口字段，所有 ID 仍为字符串；无主有效期时明确返回 `"primary_period": null`，不省略该字段。

- 主有效期：该卡未取消的有效期中 `expires_at` 最早者；截止相同取 ID 较大者。包含已过期项目，忽略 `cancelled`。
- `total`：所有未归档卡片数，包含无主有效期的卡。
- `no_period`：没有任何未取消有效期的卡片数，包含“只有已取消有效期”的卡。
- `expired`：主有效期 `expires_at <= 当前时刻`。不信任数据库中可能尚未随时间更新的 `status`。
- `due_soon`：主有效期未过期，且到期本地日期与今天的日历日差在 **0–7**（含端点）。`date` 使用 `local_expires_date`，`datetime` 使用到期时刻在用户 IANA 时区中的日期；不是“未来 7×24 小时”。今天到期计入临期，精确截止时刻起计入过期。

每张卡只归类一次；超过 7 天仍有效的卡只计入 `total`。单次响应的统计与分页来自同一 SQLite 读快照、同一时刻；不同请求之间数据或时间变化时统计可以变化。聚合失败返回现有错误格式，不伪造为 0 或“无有效期”。

此接口没有前端“100 张聚合上限”，也不接受 `status` / 搜索 / 排序参数。归档卡仍通过 `/api/v1/sims` 获取，客户端过滤。Mini App 布局、双主题及工程约束见 [专项开发规范](docs/MINI_APP_DEVELOPMENT.md)。


## Docker 部署

```sh
cp .env.example .env
chmod 600 .env
# 编辑 .env 中的 Token、真实白名单及可选配置。
docker compose build
docker compose up -d
curl -fsS http://127.0.0.1:8080/readyz
curl -fsSI http://127.0.0.1:${MINIAPP_HTTP_PORT:-8081}/app/
docker compose logs --tail=100 app web
```

Compose 在宿主读取 `.env` 用于变量插值，只把列出的变量注入进程；不挂载该文件。宿主已 export 的同名变量可覆盖 Compose `.env`，这与本地 Token 加载器的优先级不同。不要分享 `docker compose config` 的完整输出，它可能展开凭据。

- `web` 是 Node 24 构建、nginx unprivileged 运行的独立静态/反向代理容器。构建产物在 `/app/`，`/api/v1/`、`/healthz`、`/readyz` 同源转发到 `app`；Go 容器不托管静态资源。
- `MINIAPP_HTTP_PORT` 仅控制宿主回环端口，默认 `8081`。外部 HTTPS 反向代理或 Tunnel 必须把整个站点转发到该端口，不要直接公开 Go 的 `8080`。
- `web` 不挂载数据卷、不接收 Bot Token；Token、白名单和数据库仍只注入 `app`。构建脚本把首帧主题脚本的 SHA-256 写入 nginx CSP，不使用 `unsafe-inline`。
- `app` 镜像固定 `golang:1.27.1-bookworm` / `gcr.io/distroless/static-debian12:nonroot`；`web` 构建固定 Node `24.22.0-alpine3.23`，运行固定 `nginxinc/nginx-unprivileged:1.28.0-alpine`，均不使用 `latest`。
- 镜像使用 uid/gid **65532:65532**，`/data` 0700 并归该用户所有；首次空命名卷继承目录权限。进程仅需要 `/data` 写权限。若改用 bind mount，先在宿主为目录设置 `chown 65532:65532` 与 `chmod 0700`；权限错误应失败，不切回 root。
- 默认命名卷 `sim-reminder-data` 挂整个 `/data`，包含 DB、WAL、SHM；不要只挂单个 `.db`。禁止多个容器/进程调度同一数据库，不要扩容 app 副本。
- 宿主只发布 `127.0.0.1:8080:8080` 与 `127.0.0.1:${MINIAPP_HTTP_PORT:-8081}:8080`；公网入口由 HTTPS 代理提供。容器 healthcheck 分别负责 Go 就绪与 web 静态入口。
- `restart: unless-stopped`；app 停止宽限 40 秒，web 10 秒。SIGTERM 先撤销 ready、停止新 API/Bot 工作与任务领取，最多等待 30 秒完成已有工作和结果写回，再关闭数据库。
- 镜像标签不是不可变 digest。部署前确认构建镜像可用并记录**实际**构建/运行 digest；本说明不宣称镜像清单或 Docker 运行已经验收。


### 停机一致性备份

SQLite 与备注/电话号码/ICCID 都在卷内，备份含敏感数据；保存在受限、加密的存储中。**不要运行中只复制 `.db`，不要在恢复时覆盖正在使用的文件。** 采用停机后完整卷快照，辅助 tar 容器仅用于运维，不是应用依赖。

以下示例使用默认卷名；若已经改名，先把 `REMIND_DATA_VOLUME` 换成实际卷名。备份目录置于当前工作目录下，避免覆盖已有同名归档：

```sh
export REMIND_DATA_VOLUME=sim-reminder-data
mkdir -p backups
chmod 700 backups
docker compose stop app
# 必须确认已退出；此命令失败时不要继续备份。
test -z "$(docker compose ps --status running -q app)"
docker run --rm --network none \
  -v "$REMIND_DATA_VOLUME:/data:ro" \
  -v "$PWD/backups:/backup" \
  alpine:3.22 sh -c 'umask 077; test ! -e /backup/remind-data.tar.gz && tar -C /data -czf /backup/remind-data.tar.gz .'
# 归档成功后重启原卷。
docker compose up -d
curl -fsS http://127.0.0.1:8080/readyz
```

检查每条命令的退出码，归档失败不能当成备份成功。归档由辅助容器 root 创建，通常为宿主 root 所有且权限 0600；按宿主权限管理策略转存，不要改成全员可读。保留应用版本/digest、备份时间和卷名，并定期演练恢复。辅助镜像需要提前可用；离线维护前先拉取它。

### 恢复到新卷

使用相同应用版本或明确支持升级的版本；旧二进制不能读取更高迁移版本。先停止当前 app，避免同一个 Token 有两个轮询/调度实例。以下 `sim-reminder-restored` 必须是**全新、空的**卷；先用 `docker volume inspect sim-reminder-restored` 确认它不存在（若存在，另取名字，不清空旧卷）：

```sh
docker compose stop app
test -z "$(docker compose ps --status running -q app)"
docker volume create sim-reminder-restored
docker run --rm --network none \
  -v sim-reminder-restored:/data \
  -v "$PWD/backups:/backup:ro" \
  alpine:3.22 sh -c 'tar -C /data -xzf /backup/remind-data.tar.gz && chown -R 65532:65532 /data && chmod 0700 /data'
export REMIND_DATA_VOLUME=sim-reminder-restored
docker compose up -d --force-recreate
curl -fsS http://127.0.0.1:8080/readyz
```

把新卷名同步写入部署使用的 `.env` 或环境管理系统，避免下一次运维意外切回原卷。原卷保持停止且不删除，确认恢复完成后再按保留策略处理。**不要运行 `docker compose down -v`**，它会删除持久化卷。

恢复验收不能只看 ready：通过 Bot/API 比较卡片、有效期、规则和续期历史的数量与内容；检查 sent 记录仍在、pending/failed/cancelled 状态正确，重启不重复发送正常完成的任务。过期租约和补发遵循上文窗口规则，恢复可能触发有效窗口内投递，应在维护计划中告知用户。建议先记录基线，再执行 restart 和备份恢复演练。

## 检查与运维验收

以下是应执行的检查命令，不代表已在任何环境执行成功：

```sh
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go build -o /tmp/remind-smoke .
docker compose build
```

race 需要合适 C 工具链；生产仍用 CGO=0。真实 Telegram 验收需要安全提供的测试 Token 与白名单本人私聊：`/start`、创建卡/未来有效期/下一分钟提醒、实际收信、按钮续期、重启检查历史。Docker 验收还需可用守护进程，检查重启持久化、完整卷备份恢复、发送期间 SIGTERM 后 40 秒内退出与租约恢复。缺少外部前提时记录“未验收”，不能用 SDK stub 测试代替真实投递结论。

应用使用结构化脱敏日志；不要主动增加 Token、initData、电话号码、ICCID、备注、请求体或含 Token 的 SDK URL 到日志。数据库和备份目前没有应用层加密；保护宿主、卷、代理配置与备份访问权限。详尽需求见 [开发规范](docs/DEVELOPMENT.md)。

## Changelog
### Unreleased — Mini App 前端
- 移除截图外圈留白、圆角框与宽度限制，保留设备安全区；首页筛选/刷新、SIM/eSIM 使用 Lucide 图标，页面返回交给 Telegram 原生按钮。
- 卡片保留归档并新增永久删除；有效期改为永久删除，均需二次确认。后端新增删除端点与级联/自增 ID 迁移，旧归档/取消接口保持原语义。
- 有效期名称可留空，精确时间输入和展示支持秒，省略秒补 `00`，移除单独的清除开始时间按钮。
- 新增独立本地测试模式 `scripts/test-miniapp.sh`：临时 SQLite、真实 API、预置测试数据，无生产凭据或 Telegram 消息投递。

- 新增 `web/` Svelte 5 + Vite + TypeScript strict 前端：同源 `/api/v1` 客户端、原始 Telegram `initData` 鉴权、10 秒超时、游标分页、401/403 只读失效和未知响应校验。
- 完成首页全量统计、独立归档筛选、双主题参考布局、卡片资料与有效期、规则版本冲突、续期 200/201 幂等语义、提醒记录和时区设置主流程。
- 新增 Temporal DST 墙上时间转换、整数最小货币单位、服务端状态优先的剩余日历天展示；手机号默认脱敏，ICCID 不在列表/详情展示。
- 新增 `web/Dockerfile` 与 nginx 同源反代配置；前端不接收 Bot Token，不修改 Go、schema、迁移或根入口文件。真实 Telegram、HTTPS、iOS/Android 真机和 Docker 守护进程验收仍依赖外部凭据与运行环境。


### Unreleased — Mini App 总览 API

- 新增已鉴权的 `GET /api/v1/overview`，返回活跃卡片全量统计、主有效期和 ID 游标分页。
- 统计与列表共用读快照和时刻，临期按用户时区的 0–7 个日历日计算；排除归档卡与取消项目，无 schema 或入口文件变更。
- 新增 [Mini App 专项规范](docs/MINI_APP_DEVELOPMENT.md)，记录双主题参考、Svelte 工程约束及现有 API 边界。

### Unreleased — 第一阶段后端

- 从演示回显 Bot 切换为私聊白名单 SIM/eSIM 管理，支持多有效期、显式提醒规则、版本化修改、软归档和可重放的确认续期。
- 引入文件型 SQLite、嵌入式校验和迁移、持久化任务/续期对话、有限重试、租约恢复、补发窗口与撤权处理。
- 提供按所有权隔离的 HTTP API、逐请求 Mini App HMAC 鉴权、存活/就绪探针；没有前端 UI 或虚假 Web App 入口。
- 增加非 root 单实例 Docker/Compose 部署、完整数据卷、二进制 healthcheck 与停机备份恢复流程。
- 明确 date/datetime、IANA/DST、周期/规则版本和非 exactly-once 投递限制；本地 Token 的 `.env` 优先行为保留。
