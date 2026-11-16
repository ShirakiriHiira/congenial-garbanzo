# 开发规范

本文件定义了后端的权威开发规范。API 字段详见 [API.md](API.md)，前端规范详见 [MINI_APP_DEVELOPMENT.md](MINI_APP_DEVELOPMENT.md)。

## 1. 核心定位与范围
**目标**: 通用服务与订阅到期管理器。一个服务可包含多个有效期，每个有效期可配置提醒规则、续费信息和续期历史。

**明确不做**:
- 自动扣款、自动续费、外部账号集成、支付功能。
- 动态字段引擎。
- 使用 Redis/PostgreSQL/ORM/微服务架构。
- 备注中存储密码、API Token 或私钥。

## 2. 领域模型

| 实体 | 语义 |
| :--- | :--- |
| **User** | Telegram 用户，绑定时区与私聊状态；数据用户隔离。 |
| **Category** | 内置 (`sim`/`vps`/`subscription`/`domain`/`other`) 或用户自定义。 |
| **ServiceItem** | 属于用户与类别；状态分为 `active`/`archived`。 |
| **SIMDetails** | SIM 专属；卡类型、号码、国家/地区、ICCID；与 ServiceItem 一对一。 |
| **Period** | 属于服务；核心属性包括日期精度、截止时间、用户时区、周期。 |
| **PeriodTerms** | 包含 `mode`/`interval`/`amount`/`currency`/`revision`。 |
| **ReminderRule** | 提醒设置；提前天数、时刻、启用状态。 |
| **Renewal** | 续期快照；记录前后截止时间、周期、金额等。 |
| **ReminderJob** | 提醒任务；包含计划时间、重试状态、执行结果等。 |
| **BotDialog** | 用户交互向导草稿；15 分钟 TTL。 |

---

## 3. 架构与技术栈
- **核心**: Go 1.27.3, `net/http`, `database/sql`, `modernc.org/sqlite`。
- **前端**: Svelte 5 + Vite + TypeScript (原生 CSS)。
- **存储**: 版本化 SQL migration；SQLite 连接池固定为 1（单实例单写者，避免分布式锁）。

### 目录职责
| 目录 | 职责 |
| :--- | :--- |
| `internal/api` | JSON API 路由、解析、校验、状态码映射 |
| `internal/auth` | `initData` 校验 |
| `internal/bot` | Telegram 交互、向导、消息渲染 |
| `internal/domain` | 时间、提醒逻辑计算 |
| `internal/reminder` | 任务调度、发送与重试 |
| `internal/service` | 核心领域逻辑、事务、乐观并发 |
| `internal/storage` | SQLite 连接、迁移 |
| `web/` | Mini App 页面逻辑 |
| `migrations/` | 数据库迁移脚本 |

---

## 4. 关键开发规则

### 并发控制
使用乐观锁机制，更新操作必须携带对应的版本号（如 `expected_cycle`, `expected_terms_revision`, `expected_revision`）。冲突时返回 `409 Conflict`，前端需处理草稿保留。

### 时间处理
- **统一 UTC** 存储；交互界面使用用户时区。
- `date` 为当地日历日排他截止；`datetime` 为 RFC3339。
- DST 跳过的时间拒绝输入，重复时间取较早值。

### 金额处理
- 金额与币种必须成对；API 使用非负整数表示最小货币单位。
- 展示时通过 `domain.FormatMoney` 还原为小数。
- 严禁四舍五入。

### 投递幂等
通过 `(period_id, rule_id, cycle, rule_revision)` 作为幂等键，避免重复发送。

### 安全要求
- 备注、日志、URL 不得出现敏感信息（密码、API Token、私钥、initData、完整号码、ICCID）。
- 永久删除不可逆，需前端进行二次确认。

---

## 5. Telegram 交互原则
- **极简入口**: 仅保留 7 个命令入口（`/start`, `/help`, `/services`, `/add`, `/reminders`, `/settings`, `/cancel`）。
- **交互规范**: 不支持用户输入 ID。向导交互由白名单动作控制。
- **消息回执**: 回调必须恰好回执一次，避免按钮状态滞留。超长正文自动切分为新消息。
- **按钮清理**: 交互后必须显式清空 `reply_markup`。

---

## 6. 测试与验收路径
```sh
# 后端测试
GOTOOLCHAIN=go1.27.3+auto go test ./...
GOTOOLCHAIN=go1.27.3+auto go test -race ./...

# 前端测试
cd web && npm run check && npm test && npm run build

# 联调
./scripts/test-miniapp.sh
```
验收路径：创建服务 → 添加有效期 → 续期 → 添加规则 → 删除服务，确保每一步的数据库状态与界面一致性。
