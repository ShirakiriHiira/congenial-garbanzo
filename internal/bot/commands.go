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

func id(v int64) string { return strconv.FormatInt(v, 10) }

func textOrDash(v *string) string {
	if v == nil || *v == "" {
		return "未填写"
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

func statusLabel(status string) string {
	switch status {
	case "active":
		return "有效"
	case "expired":
		return "已过期"
	case "archived":
		return "已归档"
	case "pending":
		return "待发送"
	case "sending":
		return "发送中"
	case "sent":
		return "已发送"
	case "failed":
		return "失败"
	case "cancelled":
		return "已取消"
	}
	return status
}

func periodExpiry(p service.Period) string {
	if p.DatePrecision == domain.PrecisionDate && p.LocalExpiresDate != nil {
		return *p.LocalExpiresDate + "（" + p.Timezone + "，当日结束前有效）"
	}
	return stamp(p.ExpiresAt, p.Timezone) + "（" + p.Timezone + "）"
}

func periodDetail(p service.Period, rules int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n状态：%s\n截止：%s\n", periodName(p), statusLabel(p.Status), periodExpiry(p))
	if p.StartsAt != nil {
		fmt.Fprintf(&b, "开始：%s\n", stamp(*p.StartsAt, p.Timezone))
	}
	b.WriteString(termsText(p.Terms) + "\n")
	switch rules {
	case 0:
		b.WriteString("提醒：未配置。")
	default:
		fmt.Fprintf(&b, "提醒：%d 条规则。", rules)
	}
	return b.String()
}

func renewalText(ctx context.Context, svc *service.Service, user service.User, r service.Renewal, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n新截止：%s\n", label, stamp(r.NewExpiresAt, r.Timezone))
	if r.NewDatePrecision == domain.PrecisionDate && r.NewLocalExpiresDate != nil {
		fmt.Fprintf(&b, "新日期：%s（%s，当日结束前有效）\n", *r.NewLocalExpiresDate, r.Timezone)
	}
	if r.Amount != nil && r.Currency != nil {
		fmt.Fprintf(&b, "金额：%s %s\n", domain.FormatMoney(*r.Amount, *r.Currency), *r.Currency)
	}
	if r.Notes != nil && *r.Notes != "" {
		fmt.Fprintf(&b, "备注：%s\n", *r.Notes)
	}
	if r.Replayed {
		b.WriteString("（该续期已完成过，显示原记录。）\n")
	}
	if p, err := svc.GetPeriod(ctx, user.ID, r.PeriodID); err == nil {
		fmt.Fprintf(&b, "当前状态：%s\n", statusLabel(p.Status))
		b.WriteString(termsText(p.Terms) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// cancelDialogText is the shared reply for a cancelled wizard.
const cancelDialogText = "已取消当前流程。"

// otherChatMessage is the shared reply for a dialog that another session owns.
const otherChatMessage = "当前流程已在其他会话中进行，请先发送 /cancel 取消。"

func helpText() string {
	return `服务与订阅提醒

/services — 我的服务
/add — 添加服务
/reminders — 提醒任务
/settings — 时区与私聊绑定
/cancel — 取消当前流程
/help — 帮助`
}
