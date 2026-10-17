# Telegram Mini App 开发规范（专项补充）

## 0. 文档定位

本文件是 [`docs/DEVELOPMENT.md`](DEVELOPMENT.md) 的**专项补充**，规定 Svelte + Vite + TypeScript 前端、Go API 对接及已授权的后端与本地测试模式扩展。

- 技术栈、总体架构、数据模型、时间语义、部署、安全与测试的**总约束仍以 `DEVELOPMENT.md` 为准**，本文件不复制。
- 本文件补充：视觉还原、页面与组件、前端工程规范、Mini App 适配、错误处理、验收及限定的本地测试入口。
- 原先仅授权只读 `GET /api/v1/overview` 的前端阶段边界，已由用户确认的新需求扩展：真正删除卡片/有效期、可空有效期名称、秒级日期时间输入及独立本地测试模式；当前边界见 §0.2。

视觉基准（两份都必须满足）：

| 文件 | 主题 | 用途 |
| --- | --- | --- |
| [`docs/miniapp.png`](miniapp.png) | 深色 | **主视觉参考** |
| [`docs/miniapp_light.png`](miniapp_light.png) | 浅色 | 同一布局的浅色基准 |

### 0.1 与总规范的已知差异

实施前记录，不静默二选一。出现新冲突时，**先书面指出冲突、给出证据与影响，等待确认**，不得自行改总规范或改后端。

| # | `DEVELOPMENT.md` | 当前仓库/本文件 | 处理 |
| --- | --- | --- | --- |
| 1 | §1、§2：第一阶段不开发 Mini App UI | 本文件规划第二阶段前端 | 本文件仅为第二阶段规范；不宣称第一阶段已完成项发生变化 |
| 2 | §4.3：有效期状态含 `renewed` | `migrations/001_initial.sql` 与 Service 只有 `active` / `expired` / `cancelled`；续期用 `cycle` + `renewals` 表达 | 前端只实现实际三态，不出现「已续期」状态徽标 |
| 3 | §3、§15.1：建议 `cmd/app/main.go`、`bot/` | 实际入口为根目录 `main.go`，Bot 在 `internal/bot/` | 按实际结构；**不修改** `main.go`、`main_test.go` |
| 4 | §2：第一阶段曾预留 Go 静态资源服务 | 当前生产 Go 不提供静态文件服务或 CORS | 当前由同源反向代理托管静态文件；独立本地测试模式见 §6.6 |
| 5 | §8：列出有效期 CRUD、提醒任务操作等能力 | 实际接口见 §3，含 `/overview` 与已授权的真正删除接口；仍无稍后提醒、全局有效期列表 | 前端只用实际接口，缺口记录在 §3.5 |

### 0.2 硬性边界

- 已授权的后端范围包括 `/overview`、日期 helper、可空有效期名称、卡片/有效期真正删除所需 API、Service、Storage、必要的版本化迁移与相应测试，以及独立 `cmd/miniapp-test` 本地测试进程；不因此扩展其他产品能力、修改生产鉴权或 Bot 命令语义。
- 真正删除需要保证被删除资源的数字 ID 永不复用，避免旧 Bot 回调或过期界面命中新建的替代资源；该保证由后端与迁移落实，前端不得依赖或自行推断 ID 分配。**不删除或覆盖**根目录 `main.go`、`main_test.go`；测试进程不复用生产启动入口。
- 生产环境不得新增鉴权绕过、测试路由、固定开发用户或假 Token。唯一例外是 §6.6 的独立回环测试进程：使用每进程生成的假凭据签发测试 initData，业务 API 仍走正常鉴权。
- 不虚构接口；业务 API 仅使用 §3 列出的路径；测试 initData 引导路径仅允许在 §6.6 的双重开发开关下调用。
- 所有业务数据以服务端响应为准，前端计算结果只用于展示。

---

## 1. 视觉还原要求

### 1.1 画布与布局

参考图为 750×861（深色）与 733×895（浅色）的手机截图，按 **2× 折算**，约 375 CSS px 宽。布局必须流式，以 320–430 CSS px 验证，不按固定画布缩放。

参考图中的整页描边、圆角外框及黑色留白属于截图展示，不是产品页面容器。当前要求页面全宽铺满视口，不设置 480px 最大宽度、不居中成卡片、不保留 16px 外侧留白或整页描边/圆角；内容内部间距与 Telegram/设备安全区仍保留。

| 区域 | 参考尺寸（CSS px，按 2× 折算） | 要求 |
| --- | --- | --- |
| 页面水平外侧留白 | 0 | 全宽布局，仅保留设备/Telegram 安全区 |
| 整页容器 | 无最大宽度、无描边、无圆角 | 不复刻截图外框；深浅两主题一致 |
| 内容内边距 | 10（左右） | 内部图标、文字、分隔线仍以此为界 |
| 顶部区域高度 | 120 | 深色 `--header-bg`，两主题都保持深色 |
| 顶部内边距 | 14 | 眉题、标题、右上圆形按钮 |
| 统计卡 | 3 等分，间距 6，高 50，圆角 10 | 见 §1.3 |
| 区块标题行 | 高 ≈39，标题基线与右侧操作垂直居中 | 左侧标题，右侧 Lucide 筛选与刷新按钮 |
| 列表行 | 高 ≈88（图标 34 + 上下留白） | 右侧 chevron |
| 分隔线 | 1px，从图标左边缘横跨到右内边距 | 与图标左边缘对齐，不缩进到文字 |
| 底部操作区 | 高 ≈52，位于页面末尾 | 说明 + 按钮；实现为吸底，见 §1.6 |

### 1.2 Design tokens

颜色只通过 CSS 自定义属性使用，组件内不得出现字面色值（`currentColor` 与透明除外）。

| Token | 深色 | 浅色 | 用途 |
| --- | --- | --- | --- |
| `--page-bg` | `#000000` | `#FCFCFC` | 页面与卡片正文背景 |
| `--header-bg` | `#172D3A` | `#172D3A` | 顶部品牌区 |
| `--stat-bg` | `#294653` | `#294653` | 统计卡、头像圆底 |
| `--text-on-dark` | `#FFFFFF` | `#FFFFFF` | 深色头部与统计卡上的标题、数字 |
| `--text` | `#FFFFFF` | `#0D0D0D` | 主文字 |
| `--text-secondary` | `#CDCDCD` | `#5D5D5D` | 副行、页脚说明 |
| `--text-muted` | `#B6C9D5` | `#B6C9D5` | 眉题、统计标签、深底次文字 |
| `--border` | `#0D0D0D` | `#EFEFEF` | 列表分隔线、底部操作区上边 |
| `--border-strong` | `#262626` | `#E2E2E2` | 外层内容卡片描边 |
| `--accent-warn` | `#FCD34D` | `#FCD34D` | 「即将到期」数字 |
| `--accent-danger` | `#FCA5A5` | `#FCA5A5` | 「已过期」数字 |
| `--icon-physical-bg` | `#DCEFE5` | `#DCEFE5` | 实体 SIM 图标底 |
| `--icon-physical-fg` | `#176B4D` | `#176B4D` | 信号格 |
| `--icon-esim-bg` | `#DDEAFE` | `#DDEAFE` | eSIM 图标底 |
| `--icon-esim-fg` | `#245BC0` | `#245BC0` | 地球 |
| `--badge-ok-bg` | `#011B0B` | `#D7F1E1` | 有效徽标底 |
| `--badge-ok-fg` | `#04B84C` | `#00702C` | 有效徽标字 |
| `--badge-warn-bg` | `#451C08` | `#FBE4D8` | 临期徽标底 |
| `--badge-warn-fg` | `#FB6A22` | `#B23F00` | 临期徽标字 |
| `--badge-danger-bg` | `#2A0F12` | `#FBE0E1` | 过期徽标底（参考图未给出，按同族推导） |
| `--badge-danger-fg` | `#FCA5A5` | `#B4232B` | 过期徽标字 |

浅色徽标刻意偏离参考图：`#008635`/`#D7F1E1` = 3.94:1，`#E25507`/`#FBE4D8` = 3.11:1，均不达 4.5:1；改用表中加深值（5.24:1 / 4.77:1）。这是**唯一允许的色彩偏差**，需在视觉验收中注明。

字号按参考图实测的墨迹高度折算，允许 ±1px：

| 元素 | 规格 |
| --- | --- |
| 眉题 `SIM MANAGER` | 10px / 600 / 大写 / `letter-spacing: .08em` |
| 页面标题 | 16px / 700 |
| 区块标题 | 15px / 700 |
| 列表主标题 | 13px / 600 |
| 列表副行 | 11px / 400 |
| 到期行 | 11px / 400（日期可用 600） |
| 徽标 | 10px / 600 |
| 统计标签 | 9px / 400 |
| 统计数字 | 14px / 700 |
| 页脚说明 | 9px / 400 |
| 按钮文字 | 11px / 600 |

参考图像素级偏小（正文约 8 CSS px 墨迹高）。**不得为追求 1:1 而使用低于 11px 的正文**：落地时正文取 13px、次要文字取 12px，并保持表格中的相对层级；视觉验收以布局、间距、层级一致为准，不要求字号逐像素一致。

以上折算值用于版式判断；实现必须满足移动端可读性下限。

圆角：内部卡片 12、统计卡 10、图标块 8、徽标 999、按钮 999；整页容器无圆角。间距使用 4px 栅格。

### 1.3 顶部与统计卡

- 眉题固定 `SIM MANAGER`；标题固定「我的通信卡」。
- 右上圆形按钮：视觉直径 33，底色 `--stat-bg`，外层触控区 ≥44×44；内容见 §5.6。
- 三张统计卡等宽，标签依次为「全部卡片」「即将到期」「已过期」。
- 数字依次使用 `--text-on-dark`、`--accent-warn`、`--accent-danger`；头部标题同样使用 `--text-on-dark`，浅色主题下统计卡仍为深色底、白字，**不反转**。
- 数字与标签左对齐，卡片内边距 12。
- 直接显示 `/overview` 的全量 `counts`，统计口径见 §3.7；无需等待所有列表页，不追加 `+`，不得用本页数量替代全量数字。接口失败时显示不可用，不伪造为 0。

### 1.4 卡片列表行

- 主标题：`name` + ` · ` + 类型标签（`实体 SIM` / `eSIM`）。
- 副行优先级：脱敏手机号 → `notes` → `operator` → `country_or_region`；全部为空时隐藏该行。
- 到期行：`<有效期标签>：YYYY-MM-DD`，日期取 `local_expires_date`；`datetime` 精度显示本地日期与 `HH:mm:ss`。
- 有效期标签：`number_validity` → 号码有效期；`plan_validity` → 套餐到期；`data_package_validity` → 流量包到期；`custom` → 使用非空 `period.name`，为空时回退到类型标签「自定义」，不回写名称。
- 无未取消有效期时显示「未设置有效期」，不显示徽标。
- 筛选、刷新、实体 SIM 与 eSIM 图标统一使用 Lucide；实体 SIM 与 eSIM 使用可区分的卡片图标，不用文字缩写、信号格或地球代替。
- 右侧 chevron 使用 `--text-secondary`，仅作视觉提示，整行可点击。
- 归档卡：整行 60% 不透明度并显示「已归档」灰徽标；默认不进入列表。
- 已取消有效期不参与主有效期选择；若卡片只剩已取消项目，按「未设置有效期」处理。

### 1.5 状态徽标

优先级：`cancelled` → `expired` → 剩余 ≤7 天 → 有效。

| 条件 | 文案 | Token |
| --- | --- | --- |
| 已取消（仅出现在详情页的有效期列表） | `已取消` | 灰（`--text-secondary` 文字 + `--border` 底） |
| 服务端 `status === "expired"` | `已过期` | danger |
| 剩余 0–7 天 | `N 天后到期`（0 天为 `今天到期`） | warn |
| 其余 | `有效` + 同行右侧 `剩余 N 天` | ok |

「剩余 N 天」为用户时区下的**日历日差**，不是 24 小时倍数。是否过期**只以服务端 `status` 为准**；客户端时钟偏差只影响剩余天数展示，不得推导出 `expired` 状态，也不得改变列表排序。

### 1.6 底部操作区

- 吸底：`position: sticky; bottom: 0`，背景 `--page-bg`，上边 1px `--border`。
- 左侧说明文字：默认显示 `/overview` 的 `共 N 张卡片`（`counts.total`）；筛选已加载项目时可显示 `已加载 N 张 / 共 M 张`，不得把局部筛选数量当作全量统计。
- 右侧主按钮：`+ 添加卡片`，深色底 `#172D3A`、白字、胶囊形；参考视觉尺寸约 74×27，但**触控区必须 ≥44×44**（用伪元素扩展）。
- 底部内边距叠加 `env(safe-area-inset-bottom)`；内容区预留等高空白，避免遮挡最后一行。
- 不使用 Telegram `MainButton`，避免与设计重复。

### 1.7 浅色主题

- 深色顶部与统计卡保持不变；页面、列表、分隔线、正文反转为浅色。
- 内部卡片与分隔线使用 `--border`；浅色主题同样不得恢复整页 1px 外框或阴影外框。
- 两套 token 必须完整定义，不允许「只覆盖部分变量」导致混色。
- 主题来源与切换见 §5.2。

### 1.8 明确排除

- 参考图页脚的「示意界面 · 示例数据」是样机水印，**不得实现**。
- 浅色图卡片下方的「以上是界面原型示意，运营商和号码均为虚构数据。」是说明文字，**不属于界面**。
- 不使用参考图之外的装饰、渐变、光照或玻璃拟态。

---

## 2. 技术栈与工程边界

沿用 `DEVELOPMENT.md` §2 的前端预留：Svelte、Vite、TypeScript、Telegram Web App 接口、原生 CSS，不引入大型 UI 组件库。

- Svelte 5（runes），TypeScript `strict`，Vite 构建。
- 目录固定为 `web/`，构建产物 `web/dist/`。
- 不引入 Tailwind 或 UI 组件库；筛选、刷新、SIM/eSIM 图标使用 Lucide，其他图标可沿用内联 SVG。
- 不引入状态管理库；使用 runes + 模块级会话上下文。
- 路由使用自写轻量 History 路由（<100 行），不引入框架。
- 仅中文文案，集中存放，便于后续国际化；本期不接 i18n 框架。
- 前端**不得**复制后端业务规则作为第二事实来源；客户端校验只做即时反馈。

---

## 3. 后端事实与对接约束

以下为当前实现与已确认变更的事实；新增未授权接口仍须先改后端并另行确认。当前授权范围见 §0.2。

### 3.1 鉴权

- 生产环境除探针外，每个请求必须且只能有一个 `Authorization: tma <原始 initData>` 头；前缀与大小写固定为 `tma `。独立测试进程的 initData 引导路由例外见 §6.6，业务 API 无例外。
- 验签严格按 Telegram 官方 Web App 数据校验规范执行，具体实现见 `internal/auth/initdata.go:21-77`：参数按键名排序、以换行连接（排除 `hash`），用 bot token 派生密钥校验签名，仅 URL 解码一次，拒绝重复键，原始数据 ≤16 KiB。
- `auth_date` 超过 `WEBAPP_AUTH_MAX_AGE`（默认 5m）或超前 30s → 401；白名单外 → 403；成功但用户不存在时按配置时区创建用户。
- 经鉴权中间件的响应统一带 `Cache-Control: no-store`。
- 凭据在有效期内可重放；服务端不提供会话、Cookie、JWT 或刷新接口。
- **`initData` 的 `auth_date` 在一次 Mini App 启动内不变**：默认 5 分钟后所有 API 都会 401，前端无法凭 JS 续期，只能提示用户重新打开 Mini App（见 §5.4）。

### 3.2 可用接口

前缀 `/api/v1`；探针不带前缀。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 存活，无需认证 |
| GET | `/readyz` | 就绪，无需认证 |
| GET | `/me` | 当前用户：`id, telegram_user_id, timezone, has_private_chat, created_at, updated_at` |
| PATCH | `/me` | `{ "timezone": "<IANA>" }` |
| GET | `/overview` | 全量统计 + 带主有效期的活跃卡片分页；仅接受 `limit,after_id`；见 §3.7 |
| GET | `/sims` | 卡片列表，游标分页 |
| POST | `/sims` | 创建卡片 |
| GET | `/sims/{id}` | 卡片详情 |
| PATCH | `/sims/{id}` | 部分更新；`status=active` 恢复归档 |
| DELETE | `/sims/{id}` | 归档，204 |
| POST | `/sims/{id}/delete` | 无查询串与请求体；真正删除卡片及关联记录，不可恢复，204 |
| GET | `/sims/{id}/periods` | 该卡有效期列表 |
| POST | `/sims/{id}/periods` | 创建有效期；`name` 可省略或为空/空白 |
| GET | `/periods/{id}` | 有效期详情 |
| PATCH | `/periods/{id}` | 必须带 `expected_cycle` |
| DELETE | `/periods/{id}?expected_cycle=N` | 取消有效期，204 |
| POST | `/periods/{id}/delete?expected_cycle=N` | 仅接受正整数 `expected_cycle`，无请求体；按当前周期真正删除有效期及关联记录，不可恢复，204 |
| GET | `/periods/{id}/rules` | 提醒规则列表 |
| POST | `/periods/{id}/rules` | 创建规则 |
| GET | `/rules/{id}` | 规则详情 |
| PATCH | `/rules/{id}` | 必须带 `expected_revision` |
| DELETE | `/rules/{id}?expected_revision=N` | 软禁用，204 |
| GET | `/periods/{id}/renewals` | 续期历史 |
| POST | `/periods/{id}/renewals` | 确认续期 |
| GET | `/reminders?status=...` | 提醒任务列表 |
| GET | `/reminders/{id}` | 任务详情 |
| POST | `/reminders/{id}/cancel` | 取消一次任务，204 |

### 3.3 字段与枚举

- 所有 ID 为十进制**字符串**；`cycle`、`revision`、`days_before`、`attempts`、`amount` 为整数。
- `expires_at`、`starts_at`、`sent_at` 等时间为 UTC RFC3339 字符串；`local_expires_date` 为 `YYYY-MM-DD` 或 `null`。
- `card_type`: `physical_sim` | `esim`；`status`(SIM): `active` | `archived`。
- `period_type`: `number_validity` | `plan_validity` | `data_package_validity` | `custom`。
- 有效期 `name` 为可选标题：创建时省略、空字符串或纯空白均接受，空白按 `""` 持久化；PATCH 用 `""` 清空；API 返回存储的 `""`，`null` 仍非法。不得自动填入类型标签或默认标题。卡片 `name` 仍必填。
- `date_precision`: `date` | `datetime`；`status`(period): `active` | `expired` | `cancelled`（非取消项在读取时动态计算）。
- 规则：`reminder_time` 严格 `HH:MM`（00:00–23:59），`enabled` 创建默认 `true`；日期时间展示可含秒，但提醒规则输入不扩展为秒级。
- 提醒任务状态：`pending` | `sending` | `sent` | `failed` | `cancelled`。
- 续期金额：整数最小货币单位，`amount` 与 `currency`（3 位大写字母）同时给出或同时省略。

### 3.4 请求、分页与错误

- 有请求体时必须 `Content-Type: application/json`；最大 64 KiB；拒绝未知字段、多段 JSON、尾随内容；仅文档列出的可选字段允许 `null`。
- PATCH 缺省字段 = 不变；显式 `null` 仅清除支持 null 的可选字段。有效期 `name` 可用 `""` 清空（不是 `null`）；卡片 `name`、`card_type`、`status`、`period_type`、`date_precision`、`expires_at` 不可清空。
- 查询串严格：未知键、重复键、空键均 400；`limit` 1–100（默认 20）；`after_id` 非负；仅 `/reminders` 接受 `status`。
- 普通列表按 ID 升序 keyset 分页，响应 `{"data":[...],"next_cursor":"<id>"或null}`；`next_cursor` 为空表示结束。`/overview` 使用 §3.7 的对象结构，游标仍在顶层。
- 单对象 `{"data":{...}}`；错误 `{"error":{"code":"...","message":"..."}}`。
- 状态码：创建 201；读取/修改 200；归档/取消/真正删除 204（无响应体）；续期新建 201、同周期同参数重放 200；400/401/403/404/409/413/415/503/500。
- 错误码：`invalid_input, unauthorized, forbidden, not_found, conflict, unavailable, unsupported_media_type, payload_too_large, method_not_allowed, internal_error`。
- 两个真正删除接口均无请求体，并校验所属用户：资源不存在或属于其他用户返回 404；有效期删除缺少、非数字或非正整数的 `expected_cycle` 返回 400，周期过期返回 409，且不删除任何记录。其他未知查询键仍按严格查询串规则拒绝。

### 3.5 当前不存在的能力（禁止调用）

全局有效期列表、搜索、排序参数、卡片名随提醒返回、批量操作、稍后提醒、提醒创建、头像转发、服务端时间、跨卡片提醒规则查询、WebSocket/SSE、文件上传、CORS 预检、静态资源服务、会话刷新。`/overview` 是限定为活跃卡片的总览，不是全局有效期列表，也不提供服务端筛选。

### 3.6 可推导但必须标明的行为

- 卡片归档会取消该卡所有 `pending/failed/sending` 任务；恢复 `active` 会给该卡所有非取消有效期 `cycle+1` 并取消任务。前端必须提示「旧链接/旧版本将失效」。
- 修改 `expires_at` 或 `date_precision` 会 `cycle+1` 并取消旧任务；只改名称/类型不会。
- 修改用户时区：所有**非取消**有效期 `cycle+1` 并取消其未发送任务；`date` 精度按新时区重算截止时刻，`datetime` 保持绝对时刻只更新展示时区。**所有缓存的周期号立刻失效**。
- 规则更新若字段无变化，`revision` 不变；有变化则 `revision+1` 并取消该规则未发送任务。
- 取消有效期不可恢复；已发送任务不能取消（409）。
- 真正删除与归档/取消不同：删除卡片级联删除其有效期、规则、提醒任务与续期历史；删除有效期级联删除该有效期的规则、提醒任务与续期历史，保留所属卡片。删除不可撤销。原有 `DELETE` 归档/取消接口继续供 Bot/API 使用，语义不变。
- 续期要求新截止 **严格晚于**旧截止与当前时间；相同周期相同参数重放返回原记录。

### 3.7 总览接口（已实现）

`GET /api/v1/overview?limit=20&after_id=0`：沿用逐请求 `tma` 鉴权、`no-store`、`limit` 默认 20 / 最大 100、非负字符串 `after_id`。未知查询参数（包括 `status`）返回 400。

空用户响应：

```json
{"data":{"counts":{"total":0,"due_soon":0,"expired":0,"no_period":0},"items":[]},"next_cursor":null}
```

`data.items` 每项包含 `sim`（与 `/sims/{id}` 相同的对象）及 `primary_period`（与 `/periods/{id}` 相同的对象或显式 `null`）。`next_cursor` 位于**顶层**，是最后一张已返回卡片的字符串 ID 或 `null`。

- 统计和列表都只包含当前用户 `status=active` 的卡片；归档卡通过现有 `/sims` 分页另取。
- 主有效期由后端选择：忽略 `cancelled`，取 `expires_at` 最小者，相同取数字 ID 最大者；已过期有效期仍可成为主有效期。
- `total` 是所有活跃卡片数，包含无主有效期的卡；`no_period` 是没有任何未取消有效期的卡片数。
- `expired` 用主有效期的排他截止与当前时刻比较（`expires_at <= now`），不直接使用存储状态。
- `due_soon` 是主有效期尚未过期，且用户时区下到期日期与今天日差为 **0–7** 的卡片数。`date` 取 `local_expires_date`，`datetime` 取到期瞬间的本地日期。今天到期属于临期；到达精确截止时刻立即属于过期。
- 每张卡只归类一次。主有效期超过 7 天的卡只计入 `total`；归档卡完全排除；只有已取消项目的卡计入 `no_period`。
- `counts` 覆盖**全部活跃卡片**，与 `limit/after_id` 无关，不存在 100 张统计截断。`items` 按卡片 ID 升序分页。
- 同次响应使用一个 SQLite 读快照、一个当前时刻；跨页请求间有数据修改或时间推进时统计可以变化。无缓存/服务端时间字段，前端刷新时以新响应为准。
- 查询或日历计算失败返回统一错误，不降级为假统计、空主有效期或部分数字。


---

## 4. 页面规划

路由基于 History API，basename 固定 `/app`，Vite base 固定 `/app/`（见 §6.6）。下表为去掉 basename 后的应用路由，不是 Go API 路径。

| 路由 | 页面 | 数据来源 | 主要动作 |
| --- | --- | --- | --- |
| `/` | 卡片列表（参考图） | `/overview`；归档视图另取 `/sims` | 进入详情、筛选、新建 |
| `/sims/new` | 新建卡片 | — | `POST /sims` |
| `/sims/:id` | 卡片详情 | `/sims/{id}`、`/sims/{id}/periods`，逐有效期取 `/periods/{id}/rules` 与 `/periods/{id}/renewals` | 编辑、归档/恢复、真正删除、续期入口 |
| `/sims/:id/edit` | 编辑卡片 | `/sims/{id}` | `PATCH`；清空可选字段用 `null` |
| `/sims/:id/periods/new` | 新建有效期 | 卡片信息 | `POST /sims/{id}/periods` |
| `/periods/:id/edit` | 编辑有效期 | `/periods/{id}` | `PATCH` + `expected_cycle` |
| `/periods/:id/rules` | 提醒规则 | `/periods/{id}/rules` | 创建、修改、启用/禁用 |
| `/periods/:id/renew` | 确认续期 | `/periods/{id}` | `POST .../renewals` |
| `/reminders` | 提醒记录 | `/reminders` | 按状态筛选、取消 |
| `/settings` | 设置 | `/me` | 时区、私聊绑定状态说明 |

每页必须定义：加载态、空态、部分失败态、错误态、返回行为、脏表单离开确认。

### 4.1 列表页

- 首次请求 `/overview?limit=20`，一次拿到全量 `counts`、本页卡片和各卡主有效期；立即渲染，不再逐卡调用 `/sims/{id}/periods` 做首页聚合。
- 按顶层 `next_cursor` 加载后续卡片页；无前端 100 张/5 页统计上限，分页未结束不影响统计的准确性。
- 筛选按钮仍为客户端 sheet：全部 / 即将到期 / 已过期 / 已归档。临期/过期仅筛选已加载的 overview 项，未加载完须标注「仅筛选已加载数据」并允许继续加载；不得添加未实现的 overview `status` 参数。
- 已归档视图通过 `/sims` 分页并按 `sim.status` 客户端过滤；此视图不重定义首页统计，需查看归档卡有效期时再进入详情。
- 刷新或重新进入时重取 `/overview`；写操作成功后刷新本页与统计，不在客户端增减全量 counts，不做后台轮询。

### 4.2 卡片详情

分区：资料、有效期、提醒规则、续期历史。
每张有效期展示类型、可选名称、本地日期、状态徽标、`cycle`，并提供编辑、续期、真正删除；Mini App 的有效期动作不再使用「取消有效期」。
卡片同时提供归档/恢复与真正删除，不能把归档伪装为删除。归档按钮需二次确认；恢复按钮需提示会取消未发送提醒。卡片和有效期删除均须二次确认，明确列出关联记录会一并删除且**不可恢复**；成功后刷新列表/统计或返回有效父页。

### 4.3 表单页

- 可清空的可选资料字段提供「清除」动作并提交 `null`，未修改字段不进入请求体；有效期标题例外：可留空并提交/持久化 `""`。开始时间不提供单独的「清除开始时间」按钮，允许直接编辑或清空输入。
- 提交中禁用按钮并阻止重复提交；成功后以响应体替换本地实体，不使用本地自增。
- 失败保留用户输入。

### 4.4 续期页

- 展示旧 `expires_at`、`local_expires_date`、`cycle`；日期时间按 `HH:mm:ss` 展示。
- 新日期必须晚于旧值和当前时间；提交必须带 `expected_cycle`。
- 201 显示「续期成功」；200 显示「该周期已提交过相同续期，已载入原记录」，不重复提示成功。
- 409 走 §7 冲突流程。

### 4.5 提醒页

- `status` 作为服务端过滤参数；分页用 `after_id`。
- 任务时间戳统一显示 `YYYY-MM-DD HH:mm:ss`；提醒规则本身仍只输入 `HH:MM`。
- 任务只含 `validity_period_id` / `reminder_rule_id`：卡片名与有效期名通过已加载缓存反查；缓存缺失时显示 `有效期 #<id>`，不得猜测。
- 取消成功后本地置为 `cancelled`；409 表示已发送，刷新该行。

### 4.6 展示规则（集中实现并单测）

1. **主有效期与统计**：直接使用 `/overview` 的 `primary_period` 与 `counts`；前端不重新扫描、选择主有效期或聚合统计。`primary_period: null` 显示「未设置有效期」。
2. **剩余天数仅用于展示**：在 `period.timezone` 下把到期日与今天提取为日历日期标签，按 UTC 日期序号之差计数；`date` 用 `local_expires_date`，`datetime` 用截止瞬间的本地日期。禁止将真实本地时刻的毫秒差除以 86400000。不据此覆盖服务端 `status` 或全量统计；时钟偏差造成不一致时提示刷新。
3. **手机号脱敏**：保留前 3–4 位与后 4 位，其余替换为 `*`；详情页默认同样脱敏，完整值只在编辑表单显示。
4. **ICCID**：列表与详情默认不展示，只在编辑表单显示。
5. **临期/过期列表筛选**：只作用于已经加载的项，结果是局部列表，不能覆盖全量 counts；分页未结束时必须标注范围。
6. **主题与剩余天数**：是客户端展示状态，不持久化，不作为请求凭证或写操作条件。

---

## 5. 组件与 Mini App 适配

### 5.1 组件划分

`AppShell`、`AppHeader`、`AvatarButton`、`StatGrid`、`SimList`、`SimRow`、`IconTile`、`ExpiryBadge`、`SectionHeader`、`FilterSheet`、`StickyFooter`、`AsyncState`（loading/empty/error）、`ErrorBanner`、`ConfirmDialog`、`FormField`、`DatePrecisionInput`、`DateTimeInput`、`MoneyInput`、`TimeZoneSelect`、`Toast`。

- 组件只通过 props 接收数据，不自行发请求。
- 列表行、徽标、统计卡不做多主题分支；主题由 CSS 变量承担。
- 图标内联 SVG，`aria-hidden="true"`；可点击图标必须有 `aria-label`。

### 5.2 主题

- 唯一真源：`Telegram.WebApp.colorScheme`；监听 `themeChanged` 更新 `document.documentElement.dataset.theme`。
- CSS：`:root[data-theme="dark"]` / `[data-theme="light"]` 定义 token；同时设置 `color-scheme`。
- 非 Telegram 环境回退 `matchMedia('(prefers-color-scheme: dark)')` 并监听变化。
- `index.html` 内联脚本在首帧前写入 `data-theme` 与页面背景色，避免白闪；该脚本不得读取业务数据。
- 切换后调用 `setHeaderColor('#172D3A')`、`setBackgroundColor(pageBg)`、`setBottomBarColor(pageBg)`。
- 不提供手动主题开关；不跟随 `themeParams` 换色（品牌色固定）。

### 5.3 头像

- 仅从 Telegram 注入的 `initData` 解析 `user.photo_url` 与 `user.first_name`（验签由服务端完成，前端不得假装校验）；**不得**使用 `initDataUnsafe` 或 Bot API 转发头像。
- 只接受 `https:` 绝对 URL；加载失败、缺失或用户隐私限制时回退为 `first_name` 首字符圆形占位；再缺失则用通用人形图标。
- 图片设置 `referrerpolicy="no-referrer"`、固定尺寸、`alt` 为用户称呼。
- 不把 URL 写入日志、localStorage 或 URL 参数。

### 5.4 启动与生命周期

- 引入 `https://telegram.org/js/telegram-web-app.js`；`ready()` + `expand()` 后挂载或首帧后调用。
- 生产环境每次请求动态读取 `Telegram.WebApp.initData`；为空则显示「请从 Telegram 内打开」。仅 §6.6 的独立开发测试模式可通过测试引导路由取得签名 initData。
- 401 处理：停止重试、保留当前只读内容、显示顶部横幅「会话已过期，请关闭并重新打开 Mini App」，禁用所有写操作。**不得**循环重试或伪造 `auth_date`。
- 返回仅使用 Telegram 原生 `BackButton`：非根路由显示并绑定返回，根路由隐藏；页面内不再绘制自定义返回按钮。
- 表单脏状态时 `enableClosingConfirmation()`，离开或提交成功后关闭。
- 不调用 `requestFullscreen`；不默认 `disableVerticalSwipes`（保留 Telegram 手势）。
- 可选 `HapticFeedback.selectionChanged()` 用于筛选与确认，失败必须静默。

### 5.5 视口与安全区

- `<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`。
- 高度优先 `100dvh`，回退 `var(--tg-viewport-stable-height, 100vh)`；监听 `viewportChanged`。
- 顶部使用 `contentSafeAreaInset.top` 或 `env(safe-area-inset-top)`；底部叠加 `safe-area-inset-bottom`；左右设备/Telegram 安全区也须保留。全宽要求只移除装饰性外边距，不移除安全区。
- 底部吸底区不得遮挡内容；滚动容器与吸底区分离。

### 5.6 右上按钮

- 显示头像；无头像时显示首字母；点击进入 `/settings`。
- 不得用 `SettingsButton` 替代；`aria-label="设置"`。
- 触控区 ≥44×44，视觉圆形 33。

### 5.7 日期与时区输入

- 「今天」与所有展示日期必须使用 `me.timezone`，不是设备时区。
- `date` 精度使用原生 `<input type="date">`，提交 `YYYY-MM-DD`。
- `datetime` 精度的到期时间、开始时间及续期到期时间均支持秒级输入（`HH:mm:ss`）；用户只输入时分时补 `:00`，不丢弃已有秒数。按 `me.timezone` 将墙上时间解析为带偏移的 RFC3339；**禁止**直接 `new Date(value).toISOString()`（会使用设备时区）。提醒规则的 `reminder_time` 仍为 `HH:MM`，不扩展为秒级。
- 时区选择使用 `Intl.supportedValuesOf('timeZone')`（不可用时提供文本输入），提交前用 `Intl.DateTimeFormat(..., {timeZone})` 试构造。
- 修改时区前显示强提示：会重算所有 date 精度有效期、递增 cycle、取消未发送提醒。

---

## 6. 前端工程规范

### 6.1 目录

```text
web/
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
├── src/
│   ├── main.ts
│   ├── App.svelte
│   ├── app.css              # token、reset、全局排版
│   ├── lib/
│   │   ├── api/             # client.ts、types.ts、errors.ts
│   │   ├── telegram/        # sdk.ts、initdata.ts、theme.ts
│   │   ├── domain/          # primary-period.ts、expiry.ts、labels.ts、mask.ts
│   │   ├── router.ts
│   │   └── session.ts
│   ├── components/
│   └── routes/
└── tests/
```

### 6.2 TypeScript

- `strict: true`，禁止 `any` 与非空断言；外部输入先校验再使用。
- API 类型手写并与 §3 对应；不使用代码生成。
- 响应解析只取已知字段；多余字段忽略。ID 保持 `string`，不转 `number`。
- 金额保持整数；禁止浮点运算与 `parseFloat`。

### 6.3 API 客户端

- 单一 `request()`：拼接 `/api/v1`、注入 `Authorization`、超时（10s）、`AbortController`、JSON 解析、错误映射。
- 204 不解析响应体。
- 列表请求封装 `fetchAllPages()`，必须处理 `next_cursor` 为 `null`，并防止游标不前进导致死循环。
- 写请求不自动重试；读请求失败也只提供手动重试。
- 401 由客户端统一触发会话过期状态，页面不各自处理。
- 错误对象：`{ status, code, message }`，页面按 `status`/`code` 分支。

### 6.4 状态

- 页面级状态用 runes；跨页只用 `session`（`me`、initData、主题）与显式传入的数据。
- 不做全局实体缓存库；详情页始终以服务端响应为准。
- 任何返回 `cycle`/`revision` 的写操作，用响应体覆盖本地值。

### 6.5 样式

- 原生 CSS，BEM 风格的类名；组件样式默认 scoped，token 与 reset 全局。
- 不使用内联样式承载主题色；例外：Telegram 颜色同步 API 的实参。
- 动画 ≤200ms，尊重 `prefers-reduced-motion`。
- 断点只用流式与 `max-width`，不写设备型号判断。

### 6.6 构建与部署

- `npm ci && npm run check && npm run test && npm run build`。
- 产物 `web/dist/`；部署前缀固定 `/app/`：Vite `base: "/app/"`，路由 basename `/app`，两者必须一致；禁止使用相对 base `"./"`。
- 反向代理：`/app/` 指向静态目录，`/app/assets/` 长缓存（`Cache-Control: public, max-age=31536000, immutable`），`/api/v1/`、`/healthz`、`/readyz` 始终直通 Go；SPA 回退限定为 `/app/*` → `/app/index.html`，不得写成全局兜底。
- `/app`（无尾斜杠）必须 301 到 `/app/`，避免相对资源解析错位。
- 开发：Vite `server.proxy` 把 `/api` 转发到 `http://127.0.0.1:8080`，规避无 CORS；dev 与 build 均使用 `/app/` base，不切换。
- 生产：静态文件与 `/api/v1` **同源**，由现有 HTTPS 反向代理或 Tunnel 提供；Go 不托管静态文件。
- 生产认证依赖 Telegram；本地完整业务联调使用 `./scripts/test-miniapp.sh`，不是 mock API。脚本启动仅绑定回环地址的 UI（`http://127.0.0.1:5180/app/`）和 API（`http://127.0.0.1:8088`），使用真实 Service/Storage、隔离的临时 SQLite 数据库和预置测试记录，不读写生产数据库，不需要真实 Bot Token，也不启动 Bot 轮询或提醒发送器；按 Ctrl-C 退出时清理临时数据库。
- 独立 `cmd/miniapp-test` 进程在启动时生成每进程随机假 Bot Token 与固定测试用户；`GET /__test/init-data` 返回 `{"initData":"<已签名原始数据>"}`，业务请求仍经过正常签名、时效和权限校验。生产进程不注册此路由，也不接受测试身份绕过。
- 前端测试引导必须同时满足 Vite `DEV` 与 `VITE_MINIAPP_TEST_MODE` 开关；测试脚本通过 Vite 代理接入上述回环 API。普通开发模式与生产构建不得请求测试引导路由，生产构建不因设置该变量而启用测试模式。
- 仓库不提交 `.env`、构建产物、真实 `initData`、Token 或用户数据。

### 6.7 安全与隐私

- `initData` 只存内存；禁止 localStorage/sessionStorage/URL。
- 禁止把 initData、Authorization、手机号、ICCID、备注写入 console、错误上报或截图。
- 不使用 `{@html}` 渲染服务端字符串。
- 由反向代理设置 CSP：至少 `default-src 'self'; script-src 'self' https://telegram.org; img-src 'self' data: blob: https://t.me https://*.telegram.org https://*.cdn-telegram.org; connect-src 'self'`。`frame-ancestors` 必须按实际嵌入方配置（Web 与移动端 WebView 的嵌入来源不同），上线前在真机确认后再收紧；具体值以实际部署为准。
- 手机号与 ICCID 默认脱敏；复制完整值只在编辑表单中提供，并提示敏感。

---

## 7. 错误处理

| 情况 | 展示 | 行为 |
| --- | --- | --- |
| 网络失败 / 超时 | 行内错误 + 重试 | 手动重试，不自动重放写请求 |
| 400 `invalid_input` | 字段级或表单级错误 | 保留输入；不猜测具体字段时给通用提示 |
| 401 `unauthorized` | 顶部持久横幅 | 进入只读，禁用写操作，提示重新打开 |
| 403 `forbidden` | 全屏说明 | 「当前账号无访问权限」；不重试 |
| 404 `not_found` | 行内/页面空态 | 提示资源不存在或不可访问，提供返回 |
| 409 `conflict` | 冲突对话框 | 重新拉取最新实体，展示版本变化，用户确认后重试 |
| 413 `payload_too_large` | 表单错误 | 提示内容过长 |
| 415 `unsupported_media_type` | 通用错误 | 视为客户端缺陷，记录非敏感上下文 |
| 503 `unavailable` | 可用性横幅 | 手动重试，不循环 |
| 500 `internal_error` | 通用错误 | 不展示响应细节；提供重试与返回 |

- 409 禁止静默重试或本地覆盖；必须让用户看到「数据已被其他操作更新」。
- 列表部分加载失败：已成功的数据照常展示，缺失聚合标注「统计不完整」，不展示为 0。
- 所有错误文案不包含 SQL、Token、URL、堆栈。
- Toast 只用于成功反馈；失败必须有可停留的错误位置。

---

## 8. 测试

### 8.1 自动化

- 单元（Vitest）：剩余天数展示（跨月、DST 时区）、标签、脱敏、错误映射、`initData` 用户解析与 URL 校验；不另写前端统计或主有效期选择算法。
- 组件（@testing-library/svelte）：列表三态、统计卡、徽标阈值、表单校验、409 对话框、会话过期只读态。
- API 客户端：请求头与路径、204、分页终止、未知字段容错、非 JSON 错误体。
- 后端新增接口由 Go 标准测试覆盖日历边界、归档/取消排除、同截止 tie-break、身份隔离、全量统计独立于分页及游标位置；删除额外覆盖 ID 不复用、级联删除、用户隔离（404）、过期周期冲突（409）及原有归档/取消语义不变。前端 mock 沿用 §3.2 契约，不替代后端或真机验收。
- 前端测试覆盖不可逆确认、空标题持久化、秒级输入及缺省秒补 00、无页面内返回与清除开始时间按钮。
- 独立测试模式覆盖真实业务写入与数据隔离、正常鉴权、生产无测试路由及前端双重开关；这些本地检查不替代 Telegram 真机验收。
- 不引入依赖真实 Telegram 的自动化；真实环境为手工验收。

### 8.2 手工验收

- iOS 与 Android Telegram 各一次；深色与浅色各一次。
- 320 / 375 / 430 CSS px 宽；系统大字体；刘海与底部手势条。
- 断网、恢复、401 过期、403 非白名单、409 双端并发修改。
- BackButton、关闭确认、滚动到底、吸底区不遮挡。
- 真机检查头像缺失、隐私限制、CDN 加载失败三种情况。

---

## 9. 验收标准

视觉：

- [ ] 375 CSS px 宽、320 与 430 三档下参考图的布局层级保持一致；更宽视口仍全宽，无 480px 限宽、截图描边或 16px 外侧留白，安全区保留。
- [ ] 所有颜色来自 token；浅色模式无残留深色文字或深色分隔线。
- [ ] 统计卡三等分、数字配色正确；列表图标、徽标、到期行、chevron、吸底区与参考一致。
- [ ] 筛选、刷新、SIM/eSIM 使用 Lucide；仅 Telegram 原生 BackButton 提供返回，不出现重复返回按钮。
- [ ] 无水印、无参考图外的装饰；浅色徽标使用可访问色并在验收记录中注明。

功能：

- [ ] 列表、详情、新建/编辑卡片、有效期、规则、续期、提醒、设置均可完成主流程。
- [ ] 业务请求路径都属于 §3.2；仅独立测试模式可请求 §6.6 的 initData 引导路由；无虚构接口或 CORS 依赖。
- [ ] `/overview` 统计覆盖全部活跃卡片，不随分页缩小；列表游标可加载至 `null`；局部筛选有范围说明，无 100 张聚合截断。
- [ ] `expected_cycle` / `expected_revision` 来自最新响应；409 后不覆盖他人修改。
- [ ] 归档/恢复与真正删除清晰区分；有效期不再提供取消动作；删除确认明确不可恢复及级联范围，404/409 不误报成功。
- [ ] 有效期标题可空且不自动填充；到期/开始/续期时间支持秒，缺省秒补 00；`datetime` 到期、续期历史与提醒任务时间显示含秒；无单独清除开始时间按钮。修改时区等操作仍提示旧周期失效。

可靠性：

- [ ] 401 不产生重试风暴；会话过期后界面可读、写操作禁用。
- [ ] 刷新、返回、重复点击不产生重复写请求。
- [ ] 无敏感信息进入日志、URL、本地存储或截图。

工程：

- [ ] `npm run check` 无 TypeScript 错误；`npm run test` 通过；`npm run build` 成功。
- [ ] 无未使用依赖；无 UI 组件库；无 `any`。
- [ ] Go 改动限定于已授权的 API/业务语义、必要迁移与独立测试入口；生产鉴权无绕过，`main.go`、`main_test.go` 未改写。
- [ ] `./scripts/test-miniapp.sh` 可在无真实 Bot Token 时启动隔离的真实业务环境；生产无 `/__test/init-data`，生产前端不启用测试身份。

---

## 10. 实施顺序

1. 工程骨架：Vite + Svelte 5 + TS strict、token、主题引导、路由、API 客户端。
2. 只读列表页：分页拉取、主有效期、统计、徽标、双主题还原。
3. 详情与表单：卡片、有效期、规则、续期、写操作与冲突处理。
4. 提醒与设置：状态筛选、取消、时区修改。
5. 会话与错误：401 只读态、部分失败、可访问性。
6. 测试与真机验收。

每次功能改动提交格式沿用 `DEVELOPMENT.md` §15.2：`type: english message`。

## 11. 已确认决策

- 部署前缀 `/app/`，basename `/app`（§6.6）。
- 副行优先级按 §1.4。
- 首版包含 `datetime` 精度（§4.3、§5.7）。
- 浅色「已过期」徽标采用 §1.2 的推导色。
- 已授权并实现只读 `/overview`（§3.7），以服务端全量统计替代前端逐卡聚合，取消 100 张/5 页统计上限。
- 统计排除归档卡，包含没有有效期的活跃卡；临期窗口固定为 0–7 个用户时区日历日，包含今天到期。
- 当前用户确认的界面要求覆盖参考截图外框：全宽、无装饰性外侧留白，保留安全区；Lucide 图标、仅原生 BackButton、空有效期标题、秒级日期时间与移除清除开始时间按钮。
- 已授权卡片与有效期真正删除 API（保留原有归档/取消语义）及独立本地测试模式；原先「不改后端」「不允许测试凭据」「纯浏览器只能 mock」的限制仅在本文件明确范围内被替代，生产安全边界不变。

无待裁决事项。后续发现与 `DEVELOPMENT.md` 的新冲突时，仍按 §0.1 记录并等待确认，不自行取舍。
