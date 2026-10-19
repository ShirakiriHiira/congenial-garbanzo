package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"
)

const pageSize = 10

// parseID and parseAfter validate decimal identifiers from commands and
// callbacks. IDs are always treated as untrusted input.
func parseID(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 1 {
		return 0, fail("ID 必须是正整数。")
	}
	return n, nil
}

func parseAfter(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 0 {
		return 0, fail("after_id 必须是非负整数。")
	}
	return n, nil
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

func textOrDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func stamp(unix int64, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Unix(unix, 0).UTC().Format(time.RFC3339)
	}
	return time.Unix(unix, 0).In(loc).Format(time.RFC3339)
}

func periodName(p service.Period) string {
	if strings.TrimSpace(p.Name) == "" {
		return "有效期"
	}
	return p.Name
}

func periodExpiry(p service.Period) string {
	if p.DatePrecision == domain.PrecisionDate && p.LocalExpiresDate != nil {
		return *p.LocalExpiresDate + "（" + p.Timezone + "，当日结束前有效）"
	}
	return stamp(p.ExpiresAt, p.Timezone) + "（" + p.Timezone + "）"
}

func periodDetail(p service.Period) string {
	var b strings.Builder
	fmt.Fprintf(&b, "有效期 #%s\n名称：%s\n类型：%s\n精度：%s\n截止：%s\n状态：%s\n版本：%d\n", id(p.ID), periodName(p), p.PeriodType, p.DatePrecision, periodExpiry(p), p.Status, p.Cycle)
	if p.StartsAt != nil {
		fmt.Fprintf(&b, "开始：%s\n", stamp(*p.StartsAt, p.Timezone))
	}
	b.WriteString(termsText(p.Terms) + "\n")
	return strings.TrimRight(b.String(), "\n")
}

func renewalText(ctx context.Context, svc *service.Service, user service.User, r service.Renewal, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s：有效期 #%s\n周期 %d → %d\n新截止：%s\n", label, id(r.ValidityPeriodID), r.PreviousCycle, r.PreviousCycle+1, stamp(r.NewExpiresAt, r.Timezone))
	if r.NewDatePrecision == domain.PrecisionDate && r.NewLocalExpiresDate != nil {
		fmt.Fprintf(&b, "新日期：%s（%s，当日结束前有效）\n", *r.NewLocalExpiresDate, r.Timezone)
	}
	if r.Amount != nil && r.Currency != nil {
		fmt.Fprintf(&b, "金额：%d %s（最小货币单位）\n", *r.Amount, *r.Currency)
	}
	if r.Notes != nil && *r.Notes != "" {
		fmt.Fprintf(&b, "备注：%s\n", *r.Notes)
	}
	if r.Replayed {
		b.WriteString("（该续期此前已完成，显示历史记录。）\n")
	}
	if p, err := svc.GetPeriod(ctx, user.ID, r.ValidityPeriodID); err == nil {
		fmt.Fprintf(&b, "当前状态：%s，版本 %d\n", p.Status, p.Cycle)
		b.WriteString(termsText(p.Terms) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// shortHelp is the terse pointer shown after a dialog ends.
const shortHelp = "常用入口：/service、/renew <period_id>、/reminders、/settings、/help。"

func helpText(zone string) string {
	return `服务与订阅有效期提醒机器人

当前时区：` + zone + `

全部操作均在私聊中通过按钮与分步向导完成：
/service — 服务菜单（列表 / 添加 / 提醒任务 / 设置）
/service list — 活跃服务列表（查看 / 编辑 / 归档 / 翻页）
/service add — 分步添加服务
/service view <id> — 服务详情（有效期、续期、规则、历史）
/service edit <id> — 分步编辑服务
/service archive <id> — 归档确认
/service period add <service_id> — 分步添加有效期
/service terms <period_id> — 分步编辑条款
/service rule add <period_id> — 分步添加提醒规则
/renew <period_id> — 分步续期（输入 → 预览 → 确认）
/reminders — 提醒任务；pending/failed 可直接取消
/settings — 当前时区与私聊绑定
/cancel — 取消当前未完成流程
/help — 本帮助

每一步都可“上一步 / 跳过（可选字段）/ 取消”；读取命令不会打断当前流程，发起写流程前会要求确认是否丢弃草稿。
date 精度格式 YYYY-MM-DD；datetime 使用带 offset 的 RFC3339。约定费用只作记录，不代表已付款。
备注只存普通说明，不要保存密码、API Token 或私钥。`
}
