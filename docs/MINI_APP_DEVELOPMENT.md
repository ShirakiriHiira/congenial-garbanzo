# Mini App 开发规范

前端专项规范。HTTP 契约见 [API.md](API.md)，领域规则见 [DEVELOPMENT.md](DEVELOPMENT.md)。

## 1. 路由

| 路由 | 页面 | 主要行为 |
| --- | --- | --- |
| `/app/` | 服务总览 | 统计、搜索、状态/类别筛选、分页、新建 |
| `/app/services/new` | 新建服务 | 通用字段；仅 SIM 类别显示 SIM 字段；`?category=sim` 预选 |
| `/app/services/:id` | 服务详情 | 资料、全部有效期、规则/历史入口、归档/恢复/删除 |
| `/app/services/:id/edit` | 编辑服务 | 改通用字段；不跨 SIM/generic kind |
| `/app/services/:id/periods/new` | 新建有效期 | 名称、精度、开始/截止、可选条款 |
| `/app/periods/:id/edit` | 编辑有效期 | 截止/精度/开始/条款；处理 cycle/terms 冲突 |
| `/app/periods/:id/rules` | 提醒规则 | 添加、编辑、启用/禁用、永久删除 |
| `/app/periods/:id/renew` | 确认续期 | 显式输入新截止；条款仅参考 |
| `/app/reminders` | 提醒记录 | 状态筛选、取消任务 |
| `/app/settings` | 设置 | 时区、会话有效期、私聊绑定 |

使用 Telegram BackButton 与浏览器历史；有未保存修改时离开前确认。Svelte 5 runes，不引入组件库。

## 2. 核心流程

1. 总览：统计卡显示全部/即将到期/已过期；列表行显示名称、类别、服务商，并逐条显示全部有效期的名称、截止与状态徽标。
2. 新建服务：名称与类别必填，服务商、管理链接、备注可空可清空。
3. 添加有效期：名称与开始可选，仅日期或精确时间，条款一次性/周期性/暂不设置。
4. 续期：显示原截止、时刻与时区，隐藏内部周期；新截止必须晚于旧截止和当前时间。
5. 规则：显示提前天数、时刻与启用状态，不显示内部版本。
6. 删除：规则、有效期、服务均二次确认并列出级联范围。

## 3. 类别与字段

| 类别 | 服务商 | 公开标识 | 额外 |
| --- | --- | --- | --- |
| SIM/eSIM | 运营商 | 不提供 | 卡类型、号码、国家/地区、ICCID |
| VPS/云服务器 | 服务商 | 主机名或 IP | 无 |
| 域名 | 注册商 | 域名 | 无 |
| 软件/AI 订阅 | 服务商 | 账号标识 | 无 |
| 其他/自定义 | 服务商 | 公开标识 | 无 |

- SIM 不使用公开标识：表单不显示、详情不显示、API 拒绝写入；辨认信息由运营商、脱敏号码与 ICCID 承载。
- 列表行 SIM 显示脱敏号码，通用服务显示公开标识/服务商；ICCID 不在列表展示。
- 详情页只显示一个状态徽标：已过期/今天到期/N 天后到期/有效；归档行降低不透明度并标注「已归档」。
- 用户可见文本不出现内部 ID、`cycle`、`revision` 或内部枚举。

## 4. 实现约定

- API 客户端集中在 `web/src/lib/api/`：`client.ts` 请求与错误分类、`decode.ts` 严格解码、`resources.ts` 资源方法；写请求不自动重试。
- 表单用 `Submitter` 防重复提交；冲突用 `ConflictFlow`：保留草稿、读取最新值，用户再次确认后才提交。
- 归档/恢复用 `PATCH /services/{id}` 的 `status`，永久删除用 `DELETE`，规则禁用用 `PATCH enabled:false`。
- 保存有效期需 `expected_cycle`，改条款需 `expected_terms_revision`，续期需 `expected_cycle`。
- 类别内联创建/改名保留，失败保留草稿；已存服务不能跨 kind。
- 金额输入接受小数，按 ISO 4217 小数位换算为整数最小货币单位；切换条款模式保留金额，除非用户显式清除。
- 备注旁提示：不要保存密码、API Token 或私钥。

## 5. 视觉、状态与移动端

- 深色头部、三张统计卡、紧凑列表、双主题、吸底操作区、Telegram 安全区；只用现有 CSS token，不写字面色值。
- 触控区 ≥44×44；在 320/375/430 CSS px 检查长名称、系统大字、刘海与底部手势，不得横向溢出。
- 空状态、加载、错误、只读、冲突复用 `AsyncState`、`ErrorBanner`、`ConfirmDialog`、`Toast`。
- 401/403 进入只读或会话失效状态且不自动重试；网络/超时错误提供手动重试。
- 搜索显式提交；筛选变化重置游标并取消旧请求；分页失败保留已加载内容。

## 6. 截图与测试

- `test_screen/` 固定文件名：`main.jpg`、`service-detail-sim.jpg`、`service-detail-generic.jpg`、`add-service-sim.jpg`、`add-service-generic.jpg`、`add-period.jpg`、`renew.jpg`、`rules.jpg`、`settings.jpg`。
- 截图不得包含真实用户数据、Token、initData、完整号码、ICCID 或备注。
- `docs/miniapp.png`、`docs/miniapp_light.png` 仅作视觉参考。
- 命令：`npm run check`、`npm test`、`npm run build`；真实界面回归用 `./scripts/test-miniapp.sh`。
- 删除/归档、冲突、五类服务、空/错误/只读状态必须可回归。

## 7. 验收清单

- [ ] 五类类别（SIM、VPS、域名、订阅、自定义）字段文案与脱敏展示正确；可创建多有效期、规则并续期。
- [ ] 首页筛选含当前/即将到期/已过期/未设置有效期/已归档/全部。
- [ ] 有效期与续期页不显示 `cycle`、`revision` 或「有效期类型」。
- [ ] 删除服务/有效期/规则均有二次确认与级联说明，取消不写；409 冲突保留草稿且不自动重试。
- [ ] 320/375/430 宽度无横向溢出，触控区满足 44×44；空状态、加载、错误、401/403 只读状态可恢复。
