package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var serviceFilterCodes = map[string]string{
	"1": "active", "2": "due_soon", "3": "expired",
	"4": "no_period", "5": "archived", "6": "all",
}

func serviceFilterFromCode(code string) (string, bool) {
	value, ok := serviceFilterCodes[code]
	return value, ok
}

var jobFilterCodes = map[string]string{"1": "pending", "2": "failed", "3": "all"}

func jobFilterFromCode(code string) (string, bool) {
	value, ok := jobFilterCodes[code]
	return value, ok
}

func categoryName(ctx context.Context, h *Handler, uid, categoryID int64) string {
	for _, c := range mustCategories(ctx, h, uid) {
		if c.ID == categoryID {
			return c.Name
		}
	}
	return "类别"
}

func expiryBadge(p service.Period, now time.Time, loc *time.Location) string {
	if p.Status != "active" {
		return statusLabel(p.Status)
	}
	var local *string
	if p.LocalExpiresDate != nil {
		local = p.LocalExpiresDate
	}
	days, err := domain.CalendarDaysUntil(domain.Expiration{ExpiresAt: time.Unix(p.ExpiresAt, 0).UTC(), DatePrecision: p.DatePrecision, LocalExpiresDate: local}, now, loc)
	if err != nil {
		return "有效"
	}
	switch {
	case days < 0:
		return "已过期"
	case days == 0:
		return "今天到期"
	case days <= service.DueSoonDays:
		return fmt.Sprintf("%d 天后到期", days)
	}
	return "有效"
}

func (h *Handler) userLocation(user service.User) *time.Location {
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (h *Handler) showOverview(ctx context.Context, b *telegram.Bot, uid int64, resp *responder, filter service.ServiceFilter, after int64) {
	text, overview, err := h.overviewPage(ctx, uid, filter, after)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	h.sendMarkup(ctx, b, resp, text, h.overviewKeyboard(ctx, uid, filter, overview, after))
}

func (h *Handler) overviewPage(ctx context.Context, uid int64, filter service.ServiceFilter, after int64) (string, service.ServiceOverview, error) {
	overview, err := h.svc.ServiceOverview(ctx, uid, after, pageSize, filter)
	if err != nil {
		return "", overview, err
	}
	user, err := h.svc.Me(ctx, uid)
	if err != nil {
		return "", overview, err
	}
	loc := h.userLocation(user)
	now := time.Now()
	var b strings.Builder
	b.WriteString("我的服务\n")
	if filter.Query != "" {
		fmt.Fprintf(&b, "搜索：%s\n", filter.Query)
	}
	fmt.Fprintf(&b, "全部 %d · 即将到期 %d · 已过期 %d\n", overview.Counts.Total, overview.Counts.DueSoon, overview.Counts.Expired)
	if len(overview.Items.Data) == 0 {
		b.WriteString("\n没有符合条件的服务。\n")
	}
	for i, item := range overview.Items.Data {
		s := item.Service
		state := "有效"
		if s.Status == "archived" {
			state = "已归档"
		}
		fmt.Fprintf(&b, "\n%d. %s [%s]\n", i+1, s.Name, state)
		provider := "未填写服务商"
		if s.Provider != nil && *s.Provider != "" {
			provider = *s.Provider
		}
		fmt.Fprintf(&b, "   %s · %s\n", categoryName(ctx, h, uid, s.CategoryID), provider)
		if item.PrimaryPeriod == nil {
			b.WriteString("   未设置有效期\n")
		} else {
			p := item.PrimaryPeriod
			fmt.Fprintf(&b, "   %s · %s · %s\n", periodName(*p), periodExpiry(*p), expiryBadge(*p, now, loc))
		}
	}
	if overview.Items.NextCursor != nil {
		b.WriteString("\n更多服务请使用下方分页按钮。\n")
	}
	return strings.TrimRight(b.String(), "\n"), overview, nil
}

func (h *Handler) overviewKeyboard(ctx context.Context, uid int64, filter service.ServiceFilter, overview service.ServiceOverview, after int64) *models.InlineKeyboardMarkup {
	code := "1"
	for key, value := range serviceFilterCodes {
		if value == filter.Status {
			code = key
		}
	}
	category := int64(0)
	if filter.CategoryID != nil {
		category = *filter.CategoryID
	}
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("当前服务", cb("service", "list", "1", id(category), "0")), button("即将到期", cb("service", "list", "2", id(category), "0")), button("已过期", cb("service", "list", "3", id(category), "0"))},
		[]models.InlineKeyboardButton{button("未设置有效期", cb("service", "list", "4", id(category), "0")), button("已归档", cb("service", "list", "5", id(category), "0")), button("全部（含归档）", cb("service", "list", "6", id(category), "0"))},
	}
	for _, item := range overview.Items.Data {
		label := item.Service.Name
		if item.PrimaryPeriod != nil {
			if item.Service.Name != "" {
				label += " · " + periodExpiry(*item.PrimaryPeriod)
			}
		}
		rows = append(rows, []models.InlineKeyboardButton{button(label, cb("service", "view", id(item.Service.ID), "0"))})
	}
	if overview.Items.NextCursor != nil {
		rows = append(rows, []models.InlineKeyboardButton{button("下一页", cb("service", "list", code, id(category), id(*overview.Items.NextCursor)))})
	}
	rows = append(rows,
		[]models.InlineKeyboardButton{button("按类别", cb("service", "cats", code, id(category))), button("添加服务", cb("dialog", "new_service"))},
		[]models.InlineKeyboardButton{button("设置", cb("settings", "view"))},
	)
	return markup(rows...)
}

func (h *Handler) showCategories(ctx context.Context, b *telegram.Bot, uid int64, resp *responder, code string) {
	status, ok := serviceFilterFromCode(code)
	if !ok {
		h.send(ctx, b, resp, "该按钮已失效，请发送 /services 重新打开。")
		return
	}
	categories := mustCategories(ctx, h, uid)
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("全部类别", cb("service", "list", code, "0", "0"))},
	}
	row := []models.InlineKeyboardButton{}
	for _, c := range categories {
		row = append(row, button(c.Name, cb("service", "list", code, id(c.ID), "0")))
		if len(row) == 2 {
			rows = append(rows, row)
			row = []models.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回服务列表", cb("service", "list", code, "0", "0"))})
	h.sendMarkup(ctx, b, resp, "按类别筛选（当前状态："+statusLabel(status)+"）", markup(rows...))
}

func (h *Handler) showServiceDetail(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, serviceID, after int64) {
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		h.send(ctx, b, resp, objectStateMessage(err))
		return
	}
	text, page, err := h.serviceDetailPage(ctx, user, item, after)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	h.sendMarkup(ctx, b, resp, text, h.serviceKeyboard(item, &page))
}

func (h *Handler) serviceDetailPage(ctx context.Context, user service.User, item service.ServiceItem, after int64) (string, service.Page[service.Period], error) {
	category := categoryName(ctx, h, user.ID, item.CategoryID)
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n类别：%s\n状态：%s\n服务商：%s\n", item.Name, category, statusLabel(item.Status), textOrDash(item.Provider))
	if item.SIMDetails == nil {
		fmt.Fprintf(&b, "公开标识：%s\n", textOrDash(item.Identifier))
	}
	fmt.Fprintf(&b, "管理链接：%s\n", textOrDash(item.ManagementURL))
	if d := item.SIMDetails; d != nil {
		kind := d.CardType
		if kind == "physical_sim" {
			kind = "实体 SIM"
		} else if kind == "esim" {
			kind = "eSIM"
		}
		fmt.Fprintf(&b, "卡类型：%s\n号码：%s\n国家/地区：%s\nICCID：%s\n", kind, textOrDash(d.PhoneNumber), textOrDash(d.CountryOrRegion), textOrDash(d.ICCID))
	}
	if item.Notes != nil {
		fmt.Fprintf(&b, "备注：%s\n", *item.Notes)
	}
	page, err := h.svc.ListPeriods(ctx, user.ID, item.ID, after, pageSize)
	if err != nil {
		return "", page, err
	}
	loc := h.userLocation(user)
	now := time.Now()
	b.WriteString("\n有效期：\n")
	if after == 0 && len(page.Data) == 0 {
		b.WriteString("（未配置有效期；不会发送提醒）\n")
	}
	for _, p := range page.Data {
		fmt.Fprintf(&b, "· %s\n  截止 %s · %s\n  %s\n", periodName(p), periodExpiry(p), expiryBadge(p, now, loc), termsText(p.Terms))
	}
	if page.NextCursor != nil {
		b.WriteString("\n更多有效期请使用下方分页按钮。")
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func (h *Handler) serviceKeyboard(item service.ServiceItem, page *service.Page[service.Period]) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{}
	if page != nil {
		for _, p := range page.Data {
			rows = append(rows, []models.InlineKeyboardButton{button(periodName(p)+" · "+periodExpiry(p), cb("period", "view", id(p.ID)))})
		}
		if page.NextCursor != nil {
			rows = append(rows, []models.InlineKeyboardButton{button("下一页有效期", cb("service", "view", id(item.ID), id(*page.NextCursor)))})
		}
	}
	if item.Status == "active" {
		rows = append(rows, []models.InlineKeyboardButton{
			button("添加有效期", cb("period", "add", id(item.ID))),
			button("编辑服务", cb("service", "edit", id(item.ID))),
		})
		rows = append(rows, []models.InlineKeyboardButton{
			button("归档服务", cb("service", "archive_ask", id(item.ID))),
			button("删除服务", cb("service", "delete_ask", id(item.ID))),
		})
	} else {
		rows = append(rows, []models.InlineKeyboardButton{
			button("恢复服务", cb("service", "restore_ask", id(item.ID))),
			button("删除服务", cb("service", "delete_ask", id(item.ID))),
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回服务列表", cb("service", "list", "1", "0", "0"))})
	return markup(rows...)
}

func (h *Handler) showPeriodDetail(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, resp, objectStateMessage(err))
		return
	}
	rules, err := h.svc.ListRules(ctx, user.ID, p.ID, 0, 100)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	h.sendMarkup(ctx, b, resp, periodDetail(p, len(rules.Data)), h.periodKeyboard(p))
}

func (h *Handler) periodKeyboard(p service.Period) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("续期", cb("period", "renew", id(p.ID))), button("提醒规则", cb("period", "rules", id(p.ID), "0"))},
		[]models.InlineKeyboardButton{button("续期历史", cb("period", "renewals", id(p.ID), "0")), button("编辑有效期", cb("period", "edit", id(p.ID)))},
	}
	rows = append(rows,
		[]models.InlineKeyboardButton{button("删除有效期", cb("period", "delete_ask", id(p.ID)))},
		[]models.InlineKeyboardButton{button("返回服务", cb("service", "view", id(p.ServiceID), "0"))},
	)
	return markup(rows...)
}

func (h *Handler) showRules(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID, after int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, resp, objectStateMessage(err))
		return
	}
	page, err := h.svc.ListRules(ctx, user.ID, periodID, after, pageSize)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "提醒规则 · %s\n截止 %s\n", periodName(p), periodExpiry(p))
	if len(page.Data) == 0 {
		text.WriteString("\n尚未配置规则；添加后会按用户时区发送提醒。\n")
	}
	for _, r := range page.Data {
		state := "已启用"
		if !r.Enabled {
			state = "已禁用"
		}
		fmt.Fprintf(&text, "\n· 提前 %d 天 %s · %s\n", r.DaysBefore, r.ReminderTime, state)
	}
	rows := [][]models.InlineKeyboardButton{}
	for _, r := range page.Data {
		state := ""
		if !r.Enabled {
			state = "（已禁用）"
		}
		rows = append(rows, []models.InlineKeyboardButton{button(fmt.Sprintf("提前 %d 天 %s%s", r.DaysBefore, r.ReminderTime, state), cb("rule", "view", id(r.ID)))})
	}
	if page.NextCursor != nil {
		rows = append(rows, []models.InlineKeyboardButton{button("下一页", cb("period", "rules", id(p.ID), id(*page.NextCursor)))})
	}
	rows = append(rows,
		[]models.InlineKeyboardButton{button("添加规则", cb("rule", "add", id(p.ID)))},
		[]models.InlineKeyboardButton{button("返回有效期", cb("period", "view", id(p.ID)))},
	)
	h.sendMarkup(ctx, b, resp, strings.TrimRight(text.String(), "\n"), markup(rows...))
}

func (h *Handler) showRuleDetail(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, ruleID int64) {
	r, err := h.svc.GetRule(ctx, user.ID, ruleID)
	if err != nil {
		h.send(ctx, b, resp, objectStateMessage(err))
		return
	}
	state := "已启用"
	if !r.Enabled {
		state = "已禁用"
	}
	text := fmt.Sprintf("提醒规则\n提前 %d 天 · %s · %s\n\n启用/禁用或删除只影响这一条规则；删除会同时移除该规则未发送与已发送的任务记录。",
		r.DaysBefore, r.ReminderTime, state)
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("编辑规则", cb("rule", "edit", id(r.ID))), button("删除规则", cb("rule", "delete_ask", id(r.ID)))},
	}
	if r.Enabled {
		rows = append(rows, []models.InlineKeyboardButton{button("禁用规则", cb("rule", "disable_ask", id(r.ID)))})
	} else {
		rows = append(rows, []models.InlineKeyboardButton{button("启用规则", cb("rule", "enable_ask", id(r.ID)))})
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回规则列表", cb("period", "rules", id(r.PeriodID), "0"))})
	h.sendMarkup(ctx, b, resp, text, markup(rows...))
}

func (h *Handler) showRenewals(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID, after int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, resp, objectStateMessage(err))
		return
	}
	page, err := h.svc.ListRenewals(ctx, user.ID, periodID, after, pageSize)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "续期历史 · %s\n", periodName(p))
	if len(page.Data) == 0 {
		text.WriteString("\n（无续期记录）")
	}
	for _, r := range page.Data {
		fmt.Fprintf(&text, "\n· %s → %s", stamp(r.PreviousExpiresAt, r.Timezone), stamp(r.NewExpiresAt, r.Timezone))
		if r.Amount != nil && r.Currency != nil {
			fmt.Fprintf(&text, "\n  金额 %d %s", *r.Amount, *r.Currency)
		}
		if r.Notes != nil && *r.Notes != "" {
			fmt.Fprintf(&text, "\n  备注：%s", *r.Notes)
		}
		text.WriteString("\n")
	}
	rows := [][]models.InlineKeyboardButton{}
	if page.NextCursor != nil {
		rows = append(rows, []models.InlineKeyboardButton{button("下一页", cb("period", "renewals", id(p.ID), id(*page.NextCursor)))})
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回有效期", cb("period", "view", id(p.ID)))})
	h.sendMarkup(ctx, b, resp, strings.TrimRight(text.String(), "\n"), markup(rows...))
}

func (h *Handler) showJobs(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, filter string, after int64) {
	page, err := h.svc.ListJobs(ctx, user.ID, filter, after, pageSize)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "提醒任务（%s）\n", statusLabel(filter))
	if filter == "all" {
		text.Reset()
		text.WriteString("提醒任务（全部）\n")
	}
	if len(page.Data) == 0 {
		text.WriteString("\n（无）")
	}
	type jobLine struct {
		job  service.Job
		name string
	}
	lines := make([]jobLine, 0, len(page.Data))
	for _, j := range page.Data {
		p, err := h.svc.GetPeriod(ctx, user.ID, j.PeriodID)
		name := "有效期"
		if err != nil {
			name = "（有效期已不存在）"
		} else {
			item, itemErr := h.svc.GetService(ctx, user.ID, p.ServiceID)
			if itemErr == nil {
				name = item.Name + " / " + periodName(p)
			} else {
				name = periodName(p)
			}
		}
		lines = append(lines, jobLine{job: j, name: name})
		fmt.Fprintf(&text, "\n· %s\n  状态：%s · 计划 %s", name, statusLabel(j.Status), stamp(j.ScheduledAt, user.Timezone))
		if j.SentAt != nil {
			fmt.Fprintf(&text, "\n  实际 %s", stamp(*j.SentAt, user.Timezone))
		}
		if j.LastErrorCode != nil {
			fmt.Fprintf(&text, "\n  原因 %s", *j.LastErrorCode)
			if *j.LastErrorCode == "no_private_chat" {
				text.WriteString("（请先私聊发送 /start 建立接收地址）")
			}
		}
		text.WriteString("\n")
	}
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("待发送", cb("job", "list", "1", "0")), button("失败", cb("job", "list", "2", "0")), button("全部", cb("job", "list", "3", "0"))},
	}
	for _, line := range lines {
		if line.job.Status == "pending" || line.job.Status == "sending" || line.job.Status == "failed" {
			rows = append(rows, []models.InlineKeyboardButton{button("取消 · "+line.name, cb("job", "cancel_ask", id(line.job.ID)))})
		}
	}
	if page.NextCursor != nil {
		code := "1"
		for k, v := range jobFilterCodes {
			if v == filter {
				code = k
			}
		}
		rows = append(rows, []models.InlineKeyboardButton{button("下一页", cb("job", "list", code, id(*page.NextCursor)))})
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回设置", cb("settings", "view"))})
	h.sendMarkup(ctx, b, resp, strings.TrimRight(text.String(), "\n"), markup(rows...))
}

func (h *Handler) showSettings(ctx context.Context, b *telegram.Bot, user service.User, resp *responder) {
	h.sendMarkup(ctx, b, resp, h.settingsText(user), markup(
		[]models.InlineKeyboardButton{button("修改时区", cb("settings", "timezone")), button("提醒任务", cb("job", "list", "1", "0"))},
		[]models.InlineKeyboardButton{button("我的服务", cb("service", "list", "1", "0", "0")), button("帮助", cb("settings", "help"))},
	))
}

func (h *Handler) settingsText(user service.User) string {
	bound := "已绑定私聊（当前会话）"
	if user.PrivateChatID == nil || *user.PrivateChatID != user.TelegramUserID {
		bound = "私聊绑定状态未知；请在本私聊发送 /start"
	}
	return fmt.Sprintf("设置\n当前时区：%s\n私聊绑定：%s\n\n修改时区时，仅日期截止会按新时区重算日历日结束，精确时间保持绝对时刻；相关有效期会推进版本，未完成提醒任务取消并重新排程。",
		user.Timezone, bound)
}

func ruleLinePlain(r service.Rule) string {
	state := "已启用"
	if !r.Enabled {
		state = "已禁用"
	}
	return fmt.Sprintf("提醒规则：提前 %d 天 %s，%s。", r.DaysBefore, r.ReminderTime, state)
}
