# HTTP API

Mini App 使用的 JSON 接口。字段清单只在本文件维护；领域规则见 [DEVELOPMENT.md](DEVELOPMENT.md)，前端规范见 [MINI_APP_DEVELOPMENT.md](MINI_APP_DEVELOPMENT.md)。

## 通用约定

- 所有业务路由位于 `/api/v1`，需要请求头 `Authorization: tma <initData>`；`initData` 校验失败返回 403。
- 只接受 `application/json` 的单个 JSON 对象，未知字段、重复查询参数、非法 ID 一律拒绝。
- ID 是十进制字符串（如 `"42"`），时间戳是 RFC3339。
- 列表使用 keyset 分页：`limit` 取 1–100，`after_id` 可选；响应体为 `{"data":[...],"next_cursor":null|"id"}`。
- 写操作不自动重试。

错误形状：

```json
{"error":{"code":"invalid_input","message":"Invalid request."}}
```

| code | HTTP |
| --- | --- |
| `invalid_input` | 400 |
| `forbidden` | 403 |
| `not_found` | 404 |
| `conflict` | 409 |
| `unavailable` | 503 |

## 端点

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET, PATCH | `/me` | 当前用户；PATCH 可改时区或 `session_ttl_seconds` |
| GET, POST | `/categories` | 列出/创建本人自定义类别 |
| PATCH | `/categories/{id}` | 改本人自定义类别名 |
| POST | `/services` | 创建服务 |
| GET | `/services/overview` | 统计 + 服务与主有效期、全部有效期摘要分页 |
| GET, PATCH | `/services/{id}` | 读取/更新；PATCH `status=archived` 归档、`status=active` 恢复 |
| DELETE | `/services/{id}` | 永久级联删除 |
| GET, POST | `/services/{id}/periods` | 列出/创建有效期 |
| GET, PATCH, DELETE | `/periods/{id}` | 读取/更新/永久删除 |
| GET, POST | `/periods/{id}/rules` | 列出/创建提醒规则 |
| GET, PATCH, DELETE | `/rules/{id}` | 读取/更新/永久删除 |
| GET, POST | `/periods/{id}/renewals` | 续期历史/确认续期 |
| GET | `/reminders` | 提醒任务列表（`status`、`limit`、`after_id`） |
| GET | `/reminders/{id}` | 任务详情 |
| POST | `/reminders/{id}/cancel` | 取消可取消任务 |

服务列表只有 `/services/overview` 一个入口，`GET /services` 返回 404。

## 字段

| 实体 | 字段 |
| --- | --- |
| Service | `id,user_id,name,category_id,provider,identifier,management_url,notes,status,created_at,updated_at,sim_details` |
| SIMDetails | `card_type,phone_number,country_or_region,iccid`；仅 SIM 类别非空 |
| Period | `id,service_id,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,terms,created_at,updated_at` |
| Terms | `mode,interval_count,interval_unit,amount,currency,revision` |
| Rule | `id,period_id,days_before,reminder_time,enabled,revision,created_at,updated_at` |
| Renewal | `id,period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at` |
| Job | `id,period_id,reminder_rule_id,cycle,rule_revision,scheduled_at,expires_at_snapshot,status,attempts,next_attempt_at,sent_at,telegram_message_id,last_error_code,created_at,updated_at` |
| User | `id,telegram_user_id,timezone,has_private_chat,session_ttl_seconds,created_at,updated_at` |
| Category | `id,user_id,code,name,kind,created_at,updated_at` |

- `identifier` 只对非 SIM 类别可用；SIM 写入非 null 返回 400 `invalid_input`。
- 有效期 `status` 不落库，由截止时间计算为 `active`/`expired`。
- 金额 `amount` 是整数最小货币单位，`currency` 是三位大写字母，两者必须成对。

## 写入规则

- 创建有效期必须给 `date_precision` 与 `expires_at`；`name`、`starts_at`、`terms` 可选。
- 修改有效期必须带 `expected_cycle`；改条款还要带 `expected_terms_revision`；`terms:null` 重置为 `mode=custom` 并递增 revision。
- 规则创建未提供 `enabled` 时视为 `true`；`(period_id,days_before,reminder_time)` 唯一。
- 续期必须显式提交新截止并带 `expected_cycle`；相同周期、相同参数重放返回既有历史（200）。
- 修改规则带 `expected_revision`；删除规则用查询参数 `expected_revision`。

## 状态筛选

可用值：`active,due_soon,expired,no_period,archived,all`。

- `active` 表示所有未归档服务，包含已过期与临期服务。
- `due_soon` 按用户时区日历日 0–7 天。
- 主有效期优先取未过期中最早的一条；全部过期时取最近过期的一条；同截止取最大 ID。
