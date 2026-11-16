# HTTP API 接口规范

本文件定义了 Mini App 与后端交互的 JSON 接口契约。
领域业务规则请参考 [DEVELOPMENT.md](DEVELOPMENT.md)，前端交互规范参考 [MINI_APP_DEVELOPMENT.md](MINI_APP_DEVELOPMENT.md)。

## 1. 通用规范

### 请求约定
- **基准路径**: 所有业务接口均位于 `/api/v1` 下。
- **身份认证**: 请求必须携带 `Authorization: tma <initData>` 请求头。`initData` 校验失败将返回 `403 Forbidden`。
- **内容类型**: 仅接受 `application/json` 格式。系统严格拒绝未知字段、重复的查询参数或非法的 ID 格式。
- **数据格式**:
    - ID 统一使用十进制字符串表示（例如 `"42"`）。
    - 时间戳统一采用 RFC3339 格式。
- **分页机制**: 列表接口使用基于 ID 的 Keyset 分页。
    - 请求参数: `limit` (取值范围 1–100), `after_id` (可选)。
    - 响应格式: `{"data": [...], "next_cursor": null | "id"}`。
- **操作原则**: 写操作（POST/PATCH/DELETE）不提供自动重试机制。

### 错误处理
错误响应统一采用以下 JSON 形状：
```json
{"error": {"code": "<code>", "message": "<message>"}}
```

| 错误码 (`code`) | HTTP 状态码 | 描述 |
| :--- | :--- | :--- |
| `invalid_input` | 400 | 请求参数无效或校验未通过 |
| `forbidden` | 403 | 鉴权失败或无操作权限 |
| `not_found` | 404 | 资源不存在 |
| `conflict` | 409 | 资源版本冲突，需处理后重试 |
| `unavailable` | 503 | 服务暂时不可用 |

---

## 2. API 端点概览

| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| GET/PATCH | `/me` | 获取当前用户信息；PATCH 用于修改时区或 `session_ttl_seconds` |
| GET/POST | `/categories` | 列出或创建自定义服务类别 |
| PATCH | `/categories/{id}` | 修改自定义类别名称 |
| POST | `/services` | 创建新服务 |
| GET | `/services/overview` | 获取服务统计信息、摘要及有效期分页 |
| GET/PATCH | `/services/{id}` | 读取或更新服务；PATCH `status=archived`/`active` 进行归档/恢复 |
| DELETE | `/services/{id}` | 永久级联删除服务 |
| GET/POST | `/services/{id}/periods` | 列出或创建有效期记录 |
| GET/PATCH/DELETE | `/periods/{id}` | 读取、更新或删除有效期 |
| GET/POST | `/periods/{id}/rules` | 列出或创建提醒规则 |
| GET/PATCH/DELETE | `/rules/{id}` | 读取、更新或删除提醒规则 |
| GET/POST | `/periods/{id}/renewals` | 查询续期历史或确认新续期 |
| GET | `/reminders` | 列出提醒任务 (`status`, `limit`, `after_id`) |
| GET | `/reminders/{id}` | 获取任务详情 |
| POST | `/reminders/{id}/cancel` | 取消指定的可取消任务 |

> [!NOTE]
> 服务列表查询统一使用 `/services/overview` 入口，直接请求 `GET /services` 将返回 404。

---

## 3. 数据实体字段清单

| 实体 | 关键字段 |
| :--- | :--- |
| **Service** | `id, user_id, name, category_id, provider, identifier, management_url, notes, status, created_at, updated_at, sim_details` |
| **SIMDetails** | `card_type, phone_number, country_or_region, iccid` (仅 SIM 类别非空) |
| **Period** | `id, service_id, name, starts_at, expires_at, date_precision, local_expires_date, timezone, status, cycle, terms, created_at, updated_at` |
| **Terms** | `mode, interval_count, interval_unit, amount, currency, revision` |
| **Rule** | `id, period_id, days_before, reminder_time, enabled, revision, created_at, updated_at` |
| **Renewal** | `id, period_id, previous_cycle, previous_expires_at, new_expires_at, previous_date_precision, new_date_precision, previous_local_expires_date, new_local_expires_date, timezone, renewed_at, amount, currency, notes, created_at` |
| **Job** | `id, period_id, reminder_rule_id, cycle, rule_revision, scheduled_at, expires_at_snapshot, status, attempts, next_attempt_at, sent_at, telegram_message_id, last_error_code, created_at, updated_at` |
| **User** | `id, telegram_user_id, timezone, has_private_chat, session_ttl_seconds, created_at, updated_at` |
| **Category** | `id, user_id, code, name, kind, created_at, updated_at` |

**约束规则**:
- **标识符**: `identifier` 仅对非 SIM 类别有效。若 SIM 类别写入该字段，返回 `400 invalid_input`。
- **状态计算**: 有效期 `status` 不存储于数据库，根据截止时间动态计算为 `active` 或 `expired`。
- **金额规则**: `amount` (最小货币单位整数) 与 `currency` (三位大写字母) 必须成对出现。

---

## 4. 写入规则与并发控制

- **有效期创建**: 必须包含 `date_precision` 与 `expires_at`；`name`, `starts_at`, `terms` 为可选。
- **有效期修改**: 必须携带 `expected_cycle` 以保证并发一致性。修改条款需携带 `expected_terms_revision`；将 `terms` 设置为 `null` 将重置为 `mode=custom` 并增加 revision。
- **提醒规则**: 创建时未提供 `enabled` 默认为 `true`。`(period_id, days_before, reminder_time)` 组合具备唯一性。
- **续期**: 必须显式提交新截止时间及 `expected_cycle`。相同周期与参数的重放请求将返回已存在的历史记录 (200)。
- **规则修改/删除**: 修改需携带 `expected_revision`；删除则通过查询参数 `expected_revision` 校验。

---

## 5. 状态筛选枚举

可用于接口查询的状态值：
`active` (所有未归档服务), `due_soon` (用户时区未来 0–7 天), `expired`, `no_period`, `archived`, `all`。

> [!TIP]
> **主有效期计算逻辑**: 优先取未过期中最早的一条；若全部过期则取最近过期的一条；若截止时间相同，则取 ID 最大的一条。
