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

// listState is the short filter/page context attached to a detail page so the
// back button restores the exact list view. The token stays well under the
// 64-byte callback limit by encoding numeric fields in base 36.
type listState struct {
	code     string
	category int64
	page     int64
}

func defaultListState() listState { return listState{code: "1", page: 1} }

func listStateFor(filter service.ServiceFilter, page int64) listState {
	state := listState{code: "1", page: page}
	for key, value := range serviceFilterCodes {
		if value == filter.Status {
			state.code = key
		}
	}
	if filter.CategoryID != nil {
		state.category = *filter.CategoryID
	}
	if state.page < 1 {
		state.page = 1
	}
	return state
}

func (s listState) token() string {
	return s.code + "." + base36(s.category) + "." + base36(s.page)
}

func (s listState) listCallback() string {
	return cb("service", "list", s.code, id(s.category), id(s.page))
}

func parseListState(token string) (listState, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return listState{}, false
	}
	if _, ok := serviceFilterFromCode(parts[0]); !ok {
		return listState{}, false
	}
	category, ok := parseBase36(parts[1])
	if !ok {
		return listState{}, false
	}
	page, ok := parseBase36(parts[2])
	if !ok || page < 1 {
		return listState{}, false
	}
	return listState{code: parts[0], category: category, page: page}, true
}

// visibleProvider drops missing providers and names that only repeat the
// service title.
func visibleProvider(s service.ServiceItem) string {
	v := visibleText(s.Provider)
	if v == "" || strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(s.Name)) {
		return ""
	}
	return v
}

func (h *Handler) userLocation(user service.User) *time.Location {
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (h *Handler) showOverview(ctx context.Context, b *telegram.Bot, uid int64, resp *responder, filter service.ServiceFilter, page int64) {
	text, overview, err := h.overviewPage(ctx, uid, filter, page)
	if err != nil {
		h.send(ctx, b, resp, serviceMessage(err))
		return
	}
	h.sendHTML(ctx, b, resp, text, h.overviewKeyboard(filter, overview))
}

func (h *Handler) overviewPage(ctx context.Context, uid int64, filter service.ServiceFilter, page int64) (string, service.ServiceOverview, error) {
	if page < 1 || page > 1<<40 {
		page = 1
	}
	overview, err := h.svc.ServiceOverview(ctx, uid, (page-1)*servicePageSize, servicePageSize, filter)
	if err != nil {
		return "", overview, err
	}
	// A shrink between renders can strand the callback on a page that no
	// longer exists; fall back to the last page.
	if len(overview.Items.Data) == 0 && page > 1 && overview.Pages > 0 && page > int64(overview.Pages) {
		page = int64(overview.Pages)
		overview, err = h.svc.ServiceOverview(ctx, uid, (page-1)*servicePageSize, servicePageSize, filter)
		if err != nil {
			return "", overview, err
		}
	}
	user, err := h.svc.Me(ctx, uid)
	if err != nil {
		return "", overview, err
	}
	loc := h.userLocation(user)
	now := time.Now()
	var b strings.Builder
	fmt.Fprintf(&b, "<b>全部 %d</b> · 即将到期 %d · 已过期 %d", overview.Counts.Total, overview.Counts.DueSoon, overview.Counts.Expired)
	if filter.Query != "" {
		fmt.Fprintf(&b, "\n搜索：%s", htmlEscape(filter.Query))
	}
	if len(overview.Items.Data) == 0 {
		b.WriteString("\n\n没有符合条件的服务。")
		return b.String(), overview, nil
	}
	for i, item := range overview.Items.Data {
		s := item.Service
		n := (overview.Page-1)*servicePageSize + i + 1
		fmt.Fprintf(&b, "\n\n<b>%d. %s</b> [%s]", n, htmlEscape(s.Name), serviceStatusBadge(s.Status, item.PrimaryPeriod, now, loc))
		meta := htmlEscape(categoryName(ctx, h, uid, s.CategoryID))
		if provider := visibleProvider(s); provider != "" {
			meta += " · " + htmlEscape(provider)
		}
		b.WriteString("\n" + meta)
		if item.PrimaryPeriod == nil {
			b.WriteString("\n未设置有效期")
		} else {
			b.WriteString("\n" + expiryLine(*item.PrimaryPeriod, now, loc))
		}
	}
	return b.String(), overview, nil
}

func (h *Handler) overviewKeyboard(filter service.ServiceFilter, overview service.ServiceOverview) *models.InlineKeyboardMarkup {
	state := listStateFor(filter, int64(overview.Page))
	category := state.category
	filterButton := func(code, label string) models.InlineKeyboardButton {
		text := label
		if code == state.code {
			text = "• " + label + " •"
		}
		return button(text, cb("service", "list", code, id(category), "1"))
	}
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{filterButton("1", "当前服务"), filterButton("2", "即将到期"), filterButton("3", "已过期")},
		[]models.InlineKeyboardButton{filterButton("4", "未设置有效期"), filterButton("5", "已归档"), filterButton("6", "全部服务")},
	}
	for _, item := range overview.Items.Data {
		rows = append(rows, []models.InlineKeyboardButton{button(item.Service.Name, cb("service", "view", id(item.Service.ID), state.token()))})
	}
	if overview.Pages > 1 {
		prev, next := button("-", cb("service", "noop")), button("-", cb("service", "noop"))
		if state.page > 1 {
			prev = button("< 上一页", cb("service", "list", state.code, id(category), id(state.page-1)))
		}
		if state.page < int64(overview.Pages) {
			next = button("下一页 >", cb("service", "list", state.code, id(category), id(state.page+1)))
		}
		rows = append(rows, []models.InlineKeyboardButton{
			prev,
			button(fmt.Sprintf("%d / %d 页", state.page, overview.Pages), cb("service", "noop")),
			next,
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		button("添加服务", cb("dialog", "new_service")),
		button("按类别", cb("service", "cats", state.code, id(category))),
		button("设置", cb("settings", "view")),
	})
	return markup(rows...)
}

func (h *Handler) showCategories(ctx context.Context, b *telegram.Bot, uid int64, resp *responder, code string) {
	status, ok := serviceFilterFromCode(code)
	if !ok {
		h.send(ctx, b, resp, expiredButtonMessage)
		return
	}
	categories := mustCategories(ctx, h, uid)
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("全部类别", cb("service", "list", code, "0", "1"))},
	}
	row := []models.InlineKeyboardButton{}
	for _, c := range categories {
		row = append(row, button(c.Name, cb("service", "list", code, id(c.ID), "1")))
		if len(row) == 2 {
			rows = append(rows, row)
			row = []models.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回服务列表", cb("service", "list", code, "0", "1"))})
	h.sendMarkup(ctx, b, resp, "按类别筛选（当前状态："+statusLabel(status)+"）", markup(rows...))
}

func (h *Handler) showServiceDetail(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, serviceID int64, state listState, after int64) {
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
	h.sendHTML(ctx, b, resp, text, h.serviceKeyboard(item, &page, state))
}

func (h *Handler) serviceDetailPage(ctx context.Context, user service.User, item service.ServiceItem, after int64) (string, service.Page[service.Period], error) {
	category := categoryName(ctx, h, user.ID, item.CategoryID)
	page, err := h.svc.ListPeriods(ctx, user.ID, item.ID, after, pageSize)
	if err != nil {
		return "", page, err
	}
	loc := h.userLocation(user)
	now := time.Now()
	var b strings.Builder
	badge := serviceStatusBadge(item.Status, primaryPeriodOf(page.Data, now), now, loc)
	fmt.Fprintf(&b, "<b>%s</b> [%s]\n", htmlEscape(item.Name), badge)

	base := "类别：" + htmlEscape(category)
	if provider := visibleProvider(item); provider != "" {
		base += " | 服务商：" + htmlEscape(provider)
	}
	if item.SIMDetails == nil {
		if v := visibleText(item.Identifier); v != "" {
			base += " | 标识：" + htmlEscape(v)
		}
	}
	b.WriteString(base + "\n")
	if d := item.SIMDetails; d != nil {
		var parts []string
		if v := visibleText(d.CountryOrRegion); v != "" {
			parts = append(parts, "地区："+htmlEscape(v))
		}
		if d.CardType != "" {
			parts = append(parts, "卡类型："+cardTypeLabel(d.CardType))
		}
		if v := visibleText(d.PhoneNumber); v != "" {
			parts = append(parts, "号码：<code>"+htmlEscape(v)+"</code>")
		}
		if v := visibleText(d.ICCID); v != "" {
			parts = append(parts, "ICCID：<code>"+htmlEscape(v)+"</code>")
		}
		if len(parts) > 0 {
			b.WriteString(strings.Join(parts, " | ") + "\n")
		}
	}
	if v := visibleText(item.ManagementURL); v != "" {
		fmt.Fprintf(&b, "管理：<a href=\"%s\">打开管理后台</a>\n", htmlEscape(v))
	}
	if v := visibleText(item.Notes); v != "" {
		fmt.Fprintf(&b, "备注：%s\n", htmlEscape(v))
	}
	b.WriteString("\n")
	if len(page.Data) == 0 {
		b.WriteString("有效期：未设置")
	} else if len(page.Data) == 1 && strings.TrimSpace(page.Data[0].Name) == "" {
		p := page.Data[0]
		fmt.Fprintf(&b, "有效期：<code>%s</code>", htmlEscape(expiryDate(p, loc)))
		if days, ok := expiryCountdown(p, now, loc); ok {
			b.WriteString(countdownLabel(days))
		}
		if terms := termsCompact(p.Terms); terms != "" {
			b.WriteString("\n" + terms)
		}
	} else {
		for i, p := range page.Data {
			fmt.Fprintf(&b, "%d. %s · <code>%s</code>", i+1, htmlEscape(periodName(p)), htmlEscape(expiryDate(p, loc)))
			if days, ok := expiryCountdown(p, now, loc); ok {
				b.WriteString(countdownLabel(days))
			}
			if terms := termsCompact(p.Terms); terms != "" {
				b.WriteString(" · " + terms)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n"), page, nil
}

func (h *Handler) serviceKeyboard(item service.ServiceItem, page *service.Page[service.Period], state listState) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{}
	if page != nil {
		singleDefault := len(page.Data) == 1 && strings.TrimSpace(page.Data[0].Name) == ""
		for _, p := range page.Data {
			label := "修改: " + periodName(p) + " · " + expiryShort(p)
			if singleDefault {
				label = "修改有效期 · " + expiryShort(p)
			}
			rows = append(rows, []models.InlineKeyboardButton{button(label, cb("period", "view", id(p.ID)))})
		}
		if page.NextCursor != nil {
			rows = append(rows, []models.InlineKeyboardButton{button("下一页有效期", cb("service", "view", id(item.ID), state.token(), base36(*page.NextCursor)))})
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
	rows = append(rows, []models.InlineKeyboardButton{button("返回服务列表", state.listCallback())})
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
		text.WriteString("\n尚未配置规则。\n")
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
	text := fmt.Sprintf("提醒规则\n提前 %d 天 · %s · %s\n\n删除规则会一并移除它的任务记录。",
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
			fmt.Fprintf(&text, "\n  金额 %s %s", domain.FormatMoney(*r.Amount, *r.Currency), *r.Currency)
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
				text.WriteString("（请先发送 /start）")
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
	bound := "已绑定私聊"
	if user.PrivateChatID == nil || *user.PrivateChatID != user.TelegramUserID {
		bound = "私聊绑定状态未知；请在本私聊发送 /start"
	}
	return fmt.Sprintf("设置\n当前时区：%s\n私聊绑定：%s", user.Timezone, bound)
}

func ruleLinePlain(r service.Rule) string {
	state := "已启用"
	if !r.Enabled {
		state = "已禁用"
	}
	return fmt.Sprintf("提醒规则：提前 %d 天 %s，%s。", r.DaysBefore, r.ReminderTime, state)
}
