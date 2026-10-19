package bot

import (
	"context"
	"fmt"
	"strings"

	"telegram.bot.remind/internal/service"

	"github.com/go-telegram/bot/models"
)

// serviceDetailPage renders one page of a service and its periods. The full
// period list is paged so a single message cannot grow without bound.
func (h *Handler) serviceDetailPage(ctx context.Context, uid int64, item service.ServiceItem, after int64) (string, service.Page[service.Period], error) {
	category := "#" + id(item.CategoryID)
	for _, c := range mustCategories(ctx, h, uid) {
		if c.ID == item.CategoryID {
			category = c.Name + " (#" + id(c.ID) + ")"
			break
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "服务 #%s\n名称：%s\n类别：%s\n状态：%s\n服务商：%s\n公开标识：%s\n管理链接：%s\n",
		id(item.ID), item.Name, category, item.Status, textOrDash(item.Provider), textOrDash(item.Identifier), textOrDash(item.ManagementURL))
	if d := item.SIMDetails; d != nil {
		fmt.Fprintf(&b, "SIM 类型：%s\n电话：%s\n国家/地区：%s\nICCID：%s\n", d.CardType, textOrDash(d.PhoneNumber), textOrDash(d.CountryOrRegion), textOrDash(d.ICCID))
	}
	if item.Notes != nil {
		fmt.Fprintf(&b, "备注：%s\n", *item.Notes)
	}
	page, err := h.svc.ListPeriods(ctx, uid, item.ID, after, pageSize)
	if err != nil {
		return "", page, err
	}
	b.WriteString("\n有效期：\n")
	if after == 0 && len(page.Data) == 0 {
		b.WriteString("（未配置；不会发送提醒）\n")
	}
	for _, p := range page.Data {
		fmt.Fprintf(&b, "#%s %s：%s [%s]，版本 %d\n  %s\n", id(p.ID), periodName(p), periodExpiry(p), p.Status, p.Cycle, termsText(p.Terms))
	}
	if page.NextCursor != nil {
		fmt.Fprintf(&b, "\n更多有效期请使用下方分页按钮。")
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

// serviceKeyboard exposes direct actions; every ID is re-validated by the
// callback handler before use.
func (h *Handler) serviceKeyboard(item service.ServiceItem, page *service.Page[service.Period]) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{}
	if page != nil {
		for _, p := range page.Data {
			if item.Status != "active" {
				rows = append(rows, pair(button("查看 #"+id(p.ID), cb("period", "view", id(p.ID)))))
				continue
			}
			rows = append(rows, pair(
				button("查看 #"+id(p.ID), cb("period", "view", id(p.ID))),
				button("续期 #"+id(p.ID), cb("period", "renew", id(p.ID))),
				button("编辑条款 #"+id(p.ID), cb("period", "terms", id(p.ID))),
			))
			rows = append(rows, pair(
				button("规则 #"+id(p.ID), cb("period", "rules", id(p.ID))),
				button("历史 #"+id(p.ID), cb("period", "renewals", id(p.ID))),
			))
		}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页有效期", cb("service", "view", id(item.ID), id(*page.NextCursor)))))
		}
	}
	rows = append(rows,
		pair(button("编辑服务", cb("service", "edit", id(item.ID))), button("有效期列表", cb("service", "periods", id(item.ID)))),
		pair(button("添加有效期", cb("period", "add", id(item.ID)))),
	)
	if item.Status == "archived" {
		rows = append(rows, pair(button("恢复服务", cb("service", "restore", id(item.ID)))))
	} else {
		rows = append(rows, pair(button("归档服务", cb("service", "archive_ask", id(item.ID)))))
	}
	rows = append(rows, pair(button("返回服务列表", cb("service", "list")), button("设置", cb("settings", "view"))))
	return markup(rows...)
}

func (h *Handler) periodKeyboard(p service.Period) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{
		pair(button("续期", cb("period", "renew", id(p.ID))), button("编辑条款", cb("period", "terms", id(p.ID)))),
		pair(button("提醒规则", cb("period", "rules", id(p.ID))), button("续期历史", cb("period", "renewals", id(p.ID)))),
		pair(button("返回服务", cb("service", "view", id(p.ServiceID)))),
	}
	return markup(rows...)
}

func periodLabel(p service.Period) string {
	return fmt.Sprintf("#%s %s %s", id(p.ID), periodName(p), periodStatusShort(p.Status))
}

func periodStatusShort(status string) string {
	switch status {
	case "active":
		return "有效"
	case "expired":
		return "已过期"
	case "cancelled":
		return "已取消"
	}
	return status
}

func (h *Handler) periodsPage(ctx context.Context, uid int64, item service.ServiceItem, after int64) (string, service.Page[service.Period], error) {
	page, err := h.svc.ListPeriods(ctx, uid, item.ID, after, pageSize)
	if err != nil {
		return "", page, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "服务 #%s %s 的有效期：\n", id(item.ID), item.Name)
	if len(page.Data) == 0 {
		b.WriteString("（无）")
	}
	for _, p := range page.Data {
		fmt.Fprintf(&b, "#%s %s\n  %s [%s] 版本 %d\n  %s\n", id(p.ID), periodName(p), periodExpiry(p), p.Status, p.Cycle, termsText(p.Terms))
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func (h *Handler) serviceListPage(ctx context.Context, uid, after int64) (string, service.Page[service.ServiceItem], error) {
	page, err := h.svc.ListServices(ctx, uid, after, pageSize, service.ServiceFilter{Status: "active"})
	if err != nil {
		return "", page, err
	}
	var b strings.Builder
	b.WriteString("服务列表（仅活跃）：\n")
	if len(page.Data) == 0 {
		b.WriteString("（无）点击“添加服务”开始；也可以使用 /service add <query> 高级快速创建。")
	}
	for _, item := range page.Data {
		fmt.Fprintf(&b, "#%s %s [%s]\n  服务商：%s\n", id(item.ID), item.Name, item.Status, textOrDash(item.Provider))
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func (h *Handler) periodDetailPage(ctx context.Context, uid int64, p service.Period) (string, error) {
	page, err := h.svc.ListRules(ctx, uid, p.ID, 0, 100)
	if err != nil {
		return "", err
	}
	status := "未配置提醒（不会发送通知）"
	if len(page.Data) > 0 {
		status = fmt.Sprintf("%d 条规则", len(page.Data))
	}
	return periodDetail(p) + "\n提醒：" + status, nil
}

func (h *Handler) rulesPage(ctx context.Context, uid int64, p service.Period, after int64) (string, service.Page[service.Rule], error) {
	page, err := h.svc.ListRules(ctx, uid, p.ID, after, pageSize)
	if err != nil {
		return "", page, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "有效期 #%s %s 的提醒规则：\n", id(p.ID), periodName(p))
	if len(page.Data) == 0 {
		b.WriteString("（未配置提醒；如需通知请添加规则）")
	}
	for _, r := range page.Data {
		fmt.Fprintf(&b, "#%s 提前 %d 天 %s  启用 %t  版本 %d\n", id(r.ID), r.DaysBefore, r.ReminderTime, r.Enabled, r.Revision)
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func renewalsText(p service.Period, page service.Page[service.Renewal]) string {
	var b strings.Builder
	fmt.Fprintf(&b, "有效期 #%s %s 的续期历史：\n", id(p.ID), periodName(p))
	if len(page.Data) == 0 {
		b.WriteString("（无）")
	}
	for _, r := range page.Data {
		fmt.Fprintf(&b, "#%s 周期 %d：%s → %s", id(r.ID), r.PreviousCycle, stamp(r.PreviousExpiresAt, r.Timezone), stamp(r.NewExpiresAt, r.Timezone))
		if r.Amount != nil && r.Currency != nil {
			fmt.Fprintf(&b, "  金额 %d %s", *r.Amount, *r.Currency)
		}
		b.WriteString("\n")
		if r.Notes != nil && *r.Notes != "" {
			fmt.Fprintf(&b, "  备注：%s\n", *r.Notes)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (h *Handler) remindersPage(ctx context.Context, user service.User, filter string, after int64) (string, service.Page[service.Job], error) {
	page, err := h.svc.ListJobs(ctx, user.ID, filter, after, pageSize)
	if err != nil {
		return "", page, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "提醒任务（%s）：\n", filter)
	if len(page.Data) == 0 {
		b.WriteString("（无）")
	}
	for _, j := range page.Data {
		fmt.Fprintf(&b, "#%s 项目 %s 周期 %d 状态 %s 尝试 %d\n", id(j.ID), id(j.ValidityPeriodID), j.Cycle, j.Status, j.Attempts)
		fmt.Fprintf(&b, "  计划 %s", stamp(j.ScheduledAt, user.Timezone))
		if j.SentAt != nil {
			fmt.Fprintf(&b, "  实际 %s", stamp(*j.SentAt, user.Timezone))
		}
		if j.LastErrorCode != nil {
			fmt.Fprintf(&b, "  原因 %s", *j.LastErrorCode)
			if *j.LastErrorCode == "no_private_chat" {
				b.WriteString("（请先私聊发送 /start 建立接收地址）")
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func (h *Handler) settingsText(ctx context.Context, user service.User) string {
	bound := "已绑定私聊（当前会话）"
	if user.PrivateChatID == nil || *user.PrivateChatID != user.TelegramUserID {
		bound = "私聊绑定状态未知；请在本私聊发送 /start"
	}
	return fmt.Sprintf("设置\n当前时区：%s\n私聊绑定：%s\n\n修改时区时，date 精度会按新时区重算日历日结束，datetime 的绝对时刻不变；相关有效期版本会推进，未完成提醒任务取消并重新排程。",
		user.Timezone, bound)
}

func ruleLinePlain(r service.Rule) string {
	return fmt.Sprintf("规则 #%s：提前 %d 天 %s，启用 %t，版本 %d。", id(r.ID), r.DaysBefore, r.ReminderTime, r.Enabled, r.Revision)
}
