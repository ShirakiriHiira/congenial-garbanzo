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
		b.WriteString("提醒：未配置提醒规则，不会发送通知。")
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
		fmt.Fprintf(&b, "金额：%d %s（最小货币单位）\n", *r.Amount, *r.Currency)
	}
	if r.Notes != nil && *r.Notes != "" {
		fmt.Fprintf(&b, "备注：%s\n", *r.Notes)
	}
	if r.Replayed {
		b.WriteString("（该续期此前已完成，显示历史记录。）\n")
	}
	if p, err := svc.GetPeriod(ctx, user.ID, r.PeriodID); err == nil {
		fmt.Fprintf(&b, "当前状态：%s\n", statusLabel(p.Status))
		b.WriteString(termsText(p.Terms) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// shortHelp is the terse pointer shown after a dialog ends.
const shortHelp = "常用入口：/services、/add、/reminders、/settings、/help。"

func helpText(zone string) string {
	return `服务与订阅有效期提醒机器人

当前时区：` + zone + `

全部操作均在私聊中通过按钮与分步向导完成：
/services — 我的服务：搜索、状态与类别筛选、分页
/add — 分步添加服务
/reminders — 提醒任务，可取消未发送任务
/settings — 时区与私聊绑定
/cancel — 取消当前未完成流程
/help — 本帮助

所有对象都从列表或详情按钮进入，不需要输入任何内部编号。每一步都可返回、跳过可选资料或取消；发起另一个写流程前会确认是否丢弃草稿。

date 精度格式 YYYY-MM-DD；datetime 直接发送本地时间，例如 2027-02-28 23:59（按当前时区），也接受带 offset 的 RFC3339。约定费用只作记录，不代表已付款。
备注只存普通说明，不要保存密码、API Token 或私钥。`
}
