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

// servicePageSize keeps one service page compact on mobile keyboards.
const servicePageSize = 5

// htmlEscaper protects user-supplied text inside Telegram HTML. Attribute
// values are escaped too, so the same replacer serves links.
var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func htmlEscape(s string) string { return htmlEscaper.Replace(s) }

// visibleText hides empty placeholders so callers can drop the whole line.
func visibleText(v *string) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(*v)
	switch s {
	case "", "未填写", "不记录":
		return ""
	}
	return s
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

func base36(v int64) string { return strconv.FormatInt(v, 36) }

func parseBase36(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 36, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

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

func expiryCountdown(p service.Period, now time.Time, loc *time.Location) (int, bool) {
	exp := domain.Expiration{ExpiresAt: time.Unix(p.ExpiresAt, 0).UTC(), DatePrecision: p.DatePrecision, LocalExpiresDate: p.LocalExpiresDate}
	days, err := domain.CalendarDaysUntil(exp, now, loc)
	return days, err == nil
}

func countdownLabel(days int) string {
	switch {
	case days > 0:
		return fmt.Sprintf("（剩 %d 天）", days)
	case days == 0:
		return "（今日到期）"
	default:
		return fmt.Sprintf("（已过期 %d 天）", -days)
	}
}

// expiryValue renders the stored cutoff as a local wall clock without seconds.
func expiryValue(p service.Period) string {
	if p.DatePrecision == domain.PrecisionDate && p.LocalExpiresDate != nil {
		return *p.LocalExpiresDate
	}
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}
	return time.Unix(p.ExpiresAt, 0).In(loc).Format("2006-01-02 15:04")
}

// expiryDate adds the zone only when the period overrides the user's default.
func expiryDate(p service.Period, userLoc *time.Location) string {
	text := expiryValue(p)
	if p.Timezone != "" && (userLoc == nil || p.Timezone != userLoc.String()) {
		text += "（" + p.Timezone + "）"
	}
	return text
}

// expiryShort is the button label form: no zone suffix and no countdown.
func expiryShort(p service.Period) string { return expiryValue(p) }

func expiryLine(p service.Period, now time.Time, userLoc *time.Location) string {
	line := periodName(p) + " · <code>" + htmlEscape(expiryDate(p, userLoc)) + "</code>"
	if days, ok := expiryCountdown(p, now, userLoc); ok {
		line += countdownLabel(days)
	}
	return line
}

// serviceStatusBadge derives the single status label shown in headings.
func serviceStatusBadge(status string, p *service.Period, now time.Time, loc *time.Location) string {
	if status == "archived" {
		return "已归档"
	}
	if p == nil {
		return "未设置"
	}
	days, ok := expiryCountdown(*p, now, loc)
	if !ok {
		return statusLabel(p.Status)
	}
	switch {
	case days < 0 || p.Status == "expired":
		return "已过期"
	case days <= service.DueSoonDays:
		return "即将到期"
	}
	return "有效"
}

// primaryPeriodOf mirrors the overview rule for a detail page that already
// loaded its periods: the earliest unexpired cutoff, else the latest expired.
func primaryPeriodOf(periods []service.Period, now time.Time) *service.Period {
	var best *service.Period
	for i := range periods {
		p := &periods[i]
		active := p.ExpiresAt > now.Unix()
		if best == nil {
			best = p
			continue
		}
		bestActive := best.ExpiresAt > now.Unix()
		if active != bestActive {
			if active {
				best = p
			}
			continue
		}
		if active && p.ExpiresAt < best.ExpiresAt || !active && p.ExpiresAt > best.ExpiresAt {
			best = p
		}
	}
	return best
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
