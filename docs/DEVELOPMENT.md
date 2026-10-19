# 服务与订阅提醒开发规范

## 目标与职责

本项目是单实例 Go 应用，用于管理服务项目、多个有效期、提醒规则、续期历史及 Telegram 提醒。SIM/eSIM 是完整支持的兼容子集；通用项目涵盖 VPS/云服务器、软件与 AI 订阅、域名及自定义类别。周期条款只记录用户约定，不自动扣款、不推算新到期日，也不代表已付款。

本文件维护总体定位、系统模型、安全边界、兼容要求和后端验收。[Mini App 专项规范](MINI_APP_DEVELOPMENT.md)仅维护页面、组件、交互、视觉与 Mini App 验收。[README](../README.md)是 API/Bot 精确契约、运行、升级、回退及实际验证记录的唯一来源，避免重复维护字段清单。

## 架构与约束

- Go、`net/http`、`database/sql`、SQLite、版本化 SQL migration、现有 Telegram Bot SDK。
- `web/` 使用 Svelte、Vite、TypeScript、原生 CSS 与 Telegram Mini App 接口。
- 单实例、单 SQLite 数据库、单调度器；保留 app + nginx、`/app/` 与同源 API 代理部署方式。
- 沿用现有鉴权、事务、分页、时间与错误处理模式；不引入 ORM、插件/动态字段引擎、自动扣款、自动续费、支付/外部账号集成或无关基础设施。
- 内部时间戳使用 UTC；日期精度到期表示用户时区日历日结束。按用户 IANA 时区计算日历日与提醒，不依赖容器时区或固定 24 小时秒数。
- 只有实际运行过的构建、测试、迁移、部署或外部集成才能记为已验证；实际结果与限制见 README。

## 数据模型

### 用户与类别

用户以内部 ID 隔离；Telegram 用户 ID 不是业务表主键。用户保存 IANA 时区与私聊绑定状态。

`service_categories` 存放内置类别与用户类别。内置类别为 `sim`（SIM 与 eSIM）、`vps`（VPS 与云服务器）、`subscription`（软件与 AI 订阅）、`domain`（域名）、`other`（其他）；内置类别无 owner。自定义类别属于单一用户且为 generic，同一用户重名返回 409，只支持创建和改名，不支持删除或动态字段。数据库约束保护内置类别不可变，并阻止类别所有者、code、kind 的变化破坏已有项目归属；服务项目触发器拒绝 NULL 类别、他人私有类别及 SIM/generic kind 切换。

### 通用服务与 SIM 专属资料

`service_items` 是通用父资源，包含用户、名称、类别、服务商、公开标识、管理链接、普通备注、状态及时间。类别必填，且必须为内置类别或归属该项目用户的类别。已使用类别不能在 SIM/generic kind 间切换。

SIM 专属字段保存在 `sim_details`，以 service item 为主键并级联删除：卡类型、号码、国家/地区、ICCID。通用项目没有 SIM detail 行。SIM 标识与普通备注不是秘密存储；任何字段均不得保存密码、API Token、私钥。

兼容边界：旧 `/sims`、旧 overview、既有 SIM Bot 命令及其请求/响应字段仅面向 SIM，绝不返回通用项目或伪造 SIM ID。共享有效期、规则、续期、提醒任务与回调通过通用 service item 关系归属，不依赖 SIM detail 表。既有 ID 与旧字段含义保持稳定。

### 有效期、条款、规则与续期

`validity_periods` 关联 `service_id`。SIM 保留原四种有效期类型；非 SIM 使用 `custom`。名称允许为空。迁移必须保留有效期 ID、cycle、日期、状态、提醒规则、续期快照、提醒任务及持久化续期对话。

`period_terms` 存放可选模式（`once`、`recurring`、`custom`）、周期数量/单位、非负整数最小货币单位金额与大写三字母币种，以及独立 revision。旧有效期没有 terms 行时读取为 custom、revision 1。terms revision 独立于 period cycle 与 reminder-rule revision。仅改条款不改变 cycle、不取消或重建任务、不改规则版本，也不重新激活已取消或永久失败任务。乐观并发检查也必须拒绝陈旧的 no-op 写入。条款仅供参考；续期金额/币种必须由用户显式提交，不从条款推断。

规则、续期历史、任务、尝试次数、租约、fencing token 与 sent 去重保持原有语义。调度器使用持久任务与有限重试。SQLite 事务与 Telegram 发送无法原子提交，因此不保证 exactly-once。

## HTTP API 与 Bot

API 位于 `/api/v1`，使用 Telegram Mini App `initData` 鉴权、字符串资源 ID、严格 JSON/查询解析、用户隔离、keyset 分页与乐观并发版本。精确实现路径、字段、状态码、Bot 语法和示例只在 README 维护，不在本文件或 Mini App 文档复制 API 字段清单。

通用 API 提供类别管理、服务 CRUD/列表、全量过滤 overview 及服务有效期操作。overview 的筛选在分页前执行，items/counts 使用同一读快照及当前时间。搜索仅检索公开 name/provider/identifier，通配符按字面处理；不能搜索备注、号码、ICCID 等敏感 SIM 信息。counts 是完整筛选集统计，不随 cursor/limit 改变。

兼容 API 仍仅限 SIM，保留旧路由、`operator` 等字段、响应结构、归档/删除及 overview 语义。旧 SIM 有效期路由及 Bot 创建命令在创建前必须验证 SIM 身份。共享 period/rule/renewal/reminder/callback 使用稳定旧 ID，并采用通用所有权关联。

Bot 保留现有 SIM 命令与回调，并增加通用 `/service` 命令组管理服务、类别、有效期与条款。旧回调 wire 格式不变；回调校验所有权及当前 period cycle。续期必须显式指定新到期时间。详细语法仅见 README。

Bot 交互层（`internal/bot`）只做传输校验、渲染与流程编排，所有写入仍调用 `internal/service` 的现有方法。`bot_dialogs` 每个用户最多一行，保存动作、固定步骤名、上限 16 KiB 的 JSON 草案、可选目标 ID 与版本（service/period/rule 外键、cycle、terms revision、rule revision）及 15 分钟 TTL；动作与步骤由代码白名单固定，不接受用户提交的名称。草案写入失败或过期后不写业务数据；业务失败保留草案，成功提交在同一事务内删除。读取命令不得清除草案；启动其他写流程前必须先经用户确认替换。权限与对象状态在每次消息、回调与提交事务内重新校验，按钮 ID 视为不可信输入；`v2:` 回调只携带动作与十进制 ID，不携带草案或敏感字段。旧 `v1:view`/`v1:renew` 及高级 query 命令继续可用，并复用同一 service 方法与幂等/版本检查。

## 安全与隐私

- 验证 Telegram Mini App HMAC 与 `auth_date`；拒绝畸形、重复、过期凭据。不得信任客户端身份或 `initDataUnsafe`。
- 一致执行 Telegram 白名单与资源所有权检查，按契约区分未认证、无权限和不存在/他人资源。
- Token、initData、手机号、ICCID、备注及请求体不得写入日志、URL、错误输出或截图。备注不得存凭据。
- 管理链接只允许无内嵌凭据的绝对 HTTP(S) URL。服务端不抓取链接；前端安全打开时使用 `noopener noreferrer`。
- 生产环境不得有鉴权绕过、固定测试用户、测试路由或测试凭据。独立 Mini App 测试进程可使用每进程随机签名密钥，但业务请求仍经过正常鉴权；测试入口不得进入生产。
- 部署使用同源 HTTPS，不直接公开 Go API。
- 备份含敏感用户数据，应限制访问并按运维策略加密。

## 迁移、升级与回退

`004_bot_dialog_steps.sql` 将单用途续期对话表泛化为多步流程（保留原行并映射为 `renew`/`await_expiration`），以 CHECK 约束动作、步骤与草案长度，并用 `ON DELETE CASCADE` 外键在服务、有效期或规则硬删除时清理悬挂对话；不可映射的旧行使迁移整体回滚。

`003_service_items.sql` 将 SIM 父表推广为通用服务、拆分 SIM 专属资料、重命名有效期父关联、增加类别和可选条款，并以 SQLite 约束保护类别归属/kind。必须保留 row ID、所有者、日期、cycle/revision、续期历史、规则、任务状态及租约/token、持久化对话与 AUTOINCREMENT 高水位。迁移事务任一步失败必须回滚。不可改写旧 migration 或其 checksum。

数据库升级到 003 后旧二进制不能原地运行；迁移器会拒绝高版本 schema，且 003 改变了表结构。回退只能先停写、保留升级后的卷，再把升级前完整卷备份恢复到一个新空卷，并用升级前二进制运行该恢复副本。升级后有写入时，先停写并另存新卷备份；决定如何保留新增记录后再恢复。不得删表、清库、重置 sequence、覆盖活动卷或静默丢弃升级后写入。

正式升级前须确认单实例停写、制作并验证包含 DB/WAL/SHM 的完整卷备份，并在可丢弃副本验证迁移和应用。缺少备份、停写未确认或迁移/完整性检查失败时不得升级正式卷。README 维护具体运维步骤。

## 后端验收

- 迁移测试覆盖空库/v1/v2、重复迁移、失败回滚、外键与 integrity 检查、SIM 字段完整映射、关联记录和 sequence 高水位保留；不得修改 001/002 迁移来绕过测试。
- 旧 SIM API/Bot 行为持续通过；旧列表在分页前过滤非 SIM。共享所有权 JOIN 不依赖 `sim_details`。
- 混合 SIM/通用 API 覆盖类别、跨用户、类别归属与 kind 校验、分页前筛选、全量统计、字面搜索、严格嵌套 JSON 与 terms 版本冲突。
- terms-only 修改前后 period cycle 与 reminder job 状态、attempts、token、cycle 不变；续期重放与回调 cycle fencing 保留既有行为。
- Bot 测试覆盖通用服务创建、独立有效期/规则、服务名称提醒、回调重放、持久化续期对话及归档/删除生命周期，同时保留旧 SIM 流程。
- Bot 交互测试覆盖：分步服务/有效期/条款/规则/时区向导最终只调用一次既有 service 方法；读取命令不清除草案；写流程替换必须确认；v2 按钮的私聊、所有权、过期、归档、取消与重复点击分支每次只应答一次且不越权写入；续期预览取消不写、确认只写一条、陈旧 cycle 拒绝且保留草案；条款修改不改变 cycle 或任务；时区修改重算 date 且保持 datetime 绝对时刻。断言基于 Telegram stub 请求与 SQLite 状态，不以源码字符串或“未 panic”作为验收。
- 生产不提供测试身份入口或鉴权绕过。本地浏览器模式不能证明真实 Telegram 投递或原生 WebView 行为。
- Mini App 页面和人工验收仅由 [专项规范](MINI_APP_DEVELOPMENT.md)定义；实现 API 字段仅由 README 定义。
