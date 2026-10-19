package bot

import (
	"context"
	"fmt"
	"strconv"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// handleCallback treats the callback payload as untrusted input. It answers
// every query exactly once, including unauthorized, stale, and malformed ones.
func (h *Handler) handleCallback(ctx context.Context, b *telegram.Bot, cq *models.CallbackQuery) {
	if cq == nil {
		return
	}
	answer := "操作失败"
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{CallbackQueryID: cq.ID, Text: answer}); err != nil {
			h.logger.Warn("telegram callback answer failed", "code", "telegram_error")
		}
	}()
	v2, ok := parseCallbackV2(cq.Data)
	if !ok {
		answer = "未知操作"
		return
	}
	answer = h.handleCallbackV2(ctx, b, cq, v2)
}

// callbackV2Identity applies the transport and allowlist checks shared by all
// v2 payloads, then authenticates the sender's private chat.
func (h *Handler) callbackV2Identity(ctx context.Context, cq *models.CallbackQuery) (service.User, models.Message, string) {
	if cq.Message.Message == nil {
		return service.User{}, models.Message{}, "请在私聊中使用"
	}
	msg := cq.Message.Message
	if msg.Chat.Type != models.ChatTypePrivate || cq.From.ID <= 0 || msg.Chat.ID != cq.From.ID {
		return service.User{}, models.Message{}, "请在私聊中使用"
	}
	uid := cq.From.ID
	if !h.svc.Allowed(uid) {
		return service.User{}, models.Message{}, "无权限使用此机器人"
	}
	user, err := h.svc.Authenticate(ctx, uid, &msg.Chat.ID)
	if err != nil {
		return service.User{}, models.Message{}, serviceMessage(err)
	}
	return user, *msg, ""
}

func (h *Handler) handleCallbackV2(ctx context.Context, b *telegram.Bot, cq *models.CallbackQuery, v2 callbackV2) string {
	user, msg, failure := h.callbackV2Identity(ctx, cq)
	if failure != "" {
		return failure
	}
	chatID := msg.Chat.ID
	switch v2.scope {
	case "dialog":
		return h.dialogCallback(ctx, b, user, chatID, v2)
	case "service":
		return h.serviceCallback(ctx, b, user, chatID, v2)
	case "period":
		return h.periodCallback(ctx, b, user, chatID, v2)
	case "rule":
		return h.ruleCallback(ctx, b, user, chatID, v2)
	case "job":
		return h.jobCallback(ctx, b, user, chatID, v2)
	case "settings":
		return h.settingsCallback(ctx, b, user, chatID, v2)
	default:
		return "未知操作"
	}
}

func (h *Handler) dialogCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	switch v2.action {
	case "cancel":
		if removed, err := h.svc.CancelDialog(ctx, user.ID); err != nil {
			return serviceMessage(err)
		} else if !removed {
			return "当前没有进行中的流程"
		}
		h.send(ctx, b, chatID, "已取消当前流程，未写入任何修改。\n\n"+shortHelp)
		return "已取消"
	case "new_service":
		h.startServiceAdd(ctx, b, user, chatID)
		return "请选择类别"
	case "confirm", "keep", "replace", "back", "skip", "set", "change_field":
	default:
		return "未知操作"
	}
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		if service.Code(err) == "not_found" {
			return "流程已过期，请重新打开详情"
		}
		return serviceMessage(err)
	}
	if d.ChatID != chatID {
		return "当前流程已在其他会话中开始；请先发送 /cancel 取消"
	}
	w := h.wizard(ctx, b, user, d)
	w.nav(ctx, v2.action, v2.args)
	switch v2.action {
	case "confirm":
		if _, err := h.svc.GetDialog(ctx, user.ID); err == nil {
			return "请按提示完成当前步骤"
		}
		return "已提交"
	case "replace":
		return "已切换流程"
	default:
		return "已更新"
	}
}

func (h *Handler) serviceCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	if v2.action == "list" {
		after := int64(0)
		if len(v2.args) > 0 {
			var ok bool
			if after, ok = callbackID(v2.args, 0); !ok {
				return "未知操作"
			}
		}
		text, page, err := h.serviceListPage(ctx, user.ID, after)
		if err != nil {
			return serviceMessage(err)
		}
		rows := [][]models.InlineKeyboardButton{}
		for _, item := range page.Data {
			rows = append(rows, pair(
				button(item.Name, cb("service", "view", id(item.ID))),
				button("编辑", cb("service", "edit", id(item.ID))),
				button("归档", cb("service", "archive_ask", id(item.ID))),
			))
		}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页", cb("service", "list", id(*page.NextCursor)))))
		}
		rows = append(rows, pair(button("添加服务", cb("dialog", "new_service")), button("设置", cb("settings", "view"))))
		h.sendMarkup(ctx, b, chatID, text, markup(rows...))
		return "服务列表已发送"
	}
	serviceID, ok := callbackID(v2.args, 0)
	if !ok {
		return "未知操作"
	}
	switch v2.action {
	case "view":
		item, err := h.svc.GetService(ctx, user.ID, serviceID)
		if err != nil {
			return objectStateMessage(err)
		}
		after := int64(0)
		if len(v2.args) > 1 {
			if after, ok = callbackID(v2.args, 1); !ok {
				return "未知操作"
			}
		}
		text, page, err := h.serviceDetailPage(ctx, user.ID, item, after)
		if err != nil {
			return serviceMessage(err)
		}
		h.sendMarkup(ctx, b, chatID, text, h.serviceKeyboard(item, &page))
		return "详情已发送"
	case "periods":
		item, err := h.svc.GetService(ctx, user.ID, serviceID)
		if err != nil {
			return objectStateMessage(err)
		}
		after := int64(0)
		if len(v2.args) > 1 {
			if after, ok = callbackID(v2.args, 1); !ok {
				return "未知操作"
			}
		}
		text, page, err := h.periodsPage(ctx, user.ID, item, after)
		if err != nil {
			return serviceMessage(err)
		}
		rows := [][]models.InlineKeyboardButton{}
		for _, p := range page.Data {
			rows = append(rows, pair(button(periodLabel(p), cb("period", "view", id(p.ID)))))
		}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页", cb("service", "periods", id(item.ID), id(*page.NextCursor)))))
		}
		rows = append(rows, pair(button("添加有效期", cb("period", "add", id(item.ID))), button("返回服务", cb("service", "view", id(item.ID)))))
		h.sendMarkup(ctx, b, chatID, text, markup(rows...))
		return "有效期已发送"
	case "edit":
		h.startServiceEdit(ctx, b, user, chatID, serviceID)
		return "请选择要修改的字段"
	case "archive_ask":
		item, err := h.svc.GetService(ctx, user.ID, serviceID)
		if err != nil {
			return objectStateMessage(err)
		}
		if item.Status == "archived" {
			h.send(ctx, b, chatID, "服务 #"+id(item.ID)+" 已经是归档状态。\n恢复后可继续管理与编辑。")
			return "服务已归档"
		}
		h.sendMarkup(ctx, b, chatID, "归档服务 #"+id(item.ID)+" "+item.Name+"？\n归档会保留有效期、续期历史与已发送记录，停止未完成的提醒任务；之后可恢复。",
			markup(pair(button("确认归档", cb("service", "archive", id(item.ID))), button("返回服务", cb("service", "view", id(item.ID))))))
		return "请确认归档"
	case "archive":
		item, err := h.svc.GetService(ctx, user.ID, serviceID)
		if err != nil {
			return objectStateMessage(err)
		}
		if item.Status == "archived" {
			h.send(ctx, b, chatID, "服务 #"+id(item.ID)+" 已经是归档状态，未重复修改。")
			return "服务已归档"
		}
		if err := h.svc.ArchiveService(ctx, user.ID, serviceID); err != nil {
			return objectStateMessage(err)
		}
		item, err = h.svc.GetService(ctx, user.ID, serviceID)
		if err != nil {
			return "服务已归档"
		}
		text, page, err := h.serviceDetailPage(ctx, user.ID, item, 0)
		if err != nil {
			return "服务已归档"
		}
		h.sendMarkup(ctx, b, chatID, text, h.serviceKeyboard(item, &page))
		return "服务已归档"
	case "restore":
		status := "active"
		item, err := h.svc.UpdateService(ctx, user.ID, serviceID, service.ServicePatch{Status: &status})
		if err != nil {
			return objectStateMessage(err)
		}
		text, page, err := h.serviceDetailPage(ctx, user.ID, item, 0)
		if err != nil {
			return "服务已恢复"
		}
		h.sendMarkup(ctx, b, chatID, text, h.serviceKeyboard(item, &page))
		return "服务已恢复"
	default:
		return "未知操作"
	}
}

func (h *Handler) periodCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	targetID, ok := callbackID(v2.args, 0)
	if !ok {
		return "未知操作"
	}
	if v2.action == "add" {
		h.startPeriodAdd(ctx, b, user, chatID, targetID)
		return "请选择有效期类型"
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, targetID)
	if err != nil {
		return objectStateMessage(err)
	}
	switch v2.action {
	case "view":
		text, err := h.periodDetailPage(ctx, user.ID, p)
		if err != nil {
			return serviceMessage(err)
		}
		h.sendMarkup(ctx, b, chatID, text, h.periodKeyboard(p))
		return "详情已发送"
	case "renew":
		if p.Status == "cancelled" {
			return "有效期已取消，不能续期"
		}
		h.startRenewWizard(ctx, b, user, chatID, p.ID)
		return "请输入新的截止"
	case "rules":
		after := int64(0)
		if len(v2.args) > 1 {
			if after, ok = callbackID(v2.args, 1); !ok {
				return "未知操作"
			}
		}
		text, page, err := h.rulesPage(ctx, user.ID, p, after)
		if err != nil {
			return serviceMessage(err)
		}
		rows := [][]models.InlineKeyboardButton{}
		for _, r := range page.Data {
			label := fmt.Sprintf("%d 天前 %s", r.DaysBefore, r.ReminderTime)
			if !r.Enabled {
				label += "（已禁用）"
			}
			row := []models.InlineKeyboardButton{button(label, cb("rule", "edit", id(r.ID)))}
			if r.Enabled {
				row = append(row, button("禁用", cb("rule", "disable_ask", id(r.ID))))
			}
			rows = append(rows, row)
		}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页", cb("period", "rules", id(p.ID), id(*page.NextCursor)))))
		}
		rows = append(rows, pair(button("添加规则", cb("rule", "add", id(p.ID))), button("返回有效期", cb("period", "view", id(p.ID)))))
		h.sendMarkup(ctx, b, chatID, text, markup(rows...))
		return "规则已发送"
	case "renewals":
		after := int64(0)
		if len(v2.args) > 1 {
			if after, ok = callbackID(v2.args, 1); !ok {
				return "未知操作"
			}
		}
		page, err := h.svc.ListRenewals(ctx, user.ID, p.ID, after, pageSize)
		if err != nil {
			return serviceMessage(err)
		}
		text := renewalsText(p, page)
		rows := [][]models.InlineKeyboardButton{}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页", cb("period", "renewals", id(p.ID), id(*page.NextCursor)))))
		}
		rows = append(rows, pair(button("返回有效期", cb("period", "view", id(p.ID)))))
		h.sendMarkup(ctx, b, chatID, text, markup(rows...))
		return "续期历史已发送"
	case "terms":
		if p.Status == "cancelled" {
			return "有效期已取消，不能编辑条款"
		}
		h.startTermsEdit(ctx, b, user, chatID, p.ID)
		return "请选择条款模式"
	default:
		return "未知操作"
	}
}

func (h *Handler) ruleCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	targetID, ok := callbackID(v2.args, 0)
	if !ok {
		return "未知操作"
	}
	if v2.action == "add" {
		found, err := h.svc.GetPeriod(ctx, user.ID, targetID)
		if err != nil {
			return objectStateMessage(err)
		}
		if found.Status == "cancelled" {
			return "有效期已取消，不能添加提醒规则"
		}
		h.startRuleAdd(ctx, b, user, chatID, found.ID)
		return "请输入提前天数"
	}
	r, err := h.svc.GetRule(ctx, user.ID, targetID)
	if err != nil {
		return objectStateMessage(err)
	}
	switch v2.action {
	case "edit":
		h.startRuleEdit(ctx, b, user, chatID, r.ID)
		return "请输入提前天数"
	case "disable_ask":
		if !r.Enabled {
			h.send(ctx, b, chatID, ruleLinePlain(r)+"\n该规则已禁用，未重复修改。")
			return "规则已禁用"
		}
		h.sendMarkup(ctx, b, chatID, "禁用规则 #"+id(r.ID)+"（提前 "+strconv.Itoa(r.DaysBefore)+" 天 "+r.ReminderTime+"）？\n禁用只停用该规则；已发送提醒保留，未完成的该规则任务会被取消。",
			markup(pair(button("确认禁用", cb("rule", "disable", id(r.ID))), button("返回规则列表", cb("period", "rules", id(r.ValidityPeriodID))))))
		return "请确认禁用"
	case "disable":
		if !r.Enabled {
			h.send(ctx, b, chatID, ruleLinePlain(r)+"\n该规则已禁用。")
			return "规则已禁用"
		}
		if err := h.svc.DisableRule(ctx, user.ID, r.ID, r.Revision); err != nil {
			if service.Code(err) == "conflict" {
				return "规则已被修改，请重新打开规则列表"
			}
			return objectStateMessage(err)
		}
		updated, err := h.svc.GetRule(ctx, user.ID, r.ID)
		if err != nil {
			return "规则已禁用"
		}
		h.sendMarkup(ctx, b, chatID, "提醒规则已禁用。\n"+ruleLinePlain(updated), markup(
			pair(button("重新启用", cb("rule", "edit", id(updated.ID))), button("返回规则列表", cb("period", "rules", id(updated.ValidityPeriodID)))),
		))
		return "规则已禁用"
	default:
		return "未知操作"
	}
}

func (h *Handler) jobCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	jobID, ok := callbackID(v2.args, 0)
	if !ok {
		return "未知操作"
	}
	if v2.action == "list" {
		after := int64(0)
		if len(v2.args) > 1 {
			if after, ok = callbackID(v2.args, 1); !ok {
				return "未知操作"
			}
		}
		text, page, err := h.remindersPage(ctx, user, "all", after)
		if err != nil {
			return serviceMessage(err)
		}
		rows := [][]models.InlineKeyboardButton{}
		for _, j := range page.Data {
			if j.Status == "pending" || j.Status == "failed" {
				rows = append(rows, pair(button("取消任务 #"+id(j.ID), cb("job", "cancel", id(j.ID)))))
			}
		}
		if page.NextCursor != nil {
			rows = append(rows, pair(button("下一页", cb("job", "list", id(*page.NextCursor)))))
		}
		rows = append(rows, pair(button("返回设置", cb("settings", "view"))))
		h.sendMarkup(ctx, b, chatID, text, markup(rows...))
		return "提醒列表已发送"
	}
	if v2.action != "cancel" {
		return "未知操作"
	}
	job, err := h.svc.GetJob(ctx, user.ID, jobID)
	if err != nil {
		return objectStateMessage(err)
	}
	if job.Status == "sent" {
		return "任务已发送，无法取消"
	}
	if job.Status == "cancelled" {
		return "任务已取消"
	}
	if err := h.svc.CancelJob(ctx, user.ID, jobID); err != nil {
		if service.Code(err) == "conflict" {
			return "任务状态已变化（可能已发送）"
		}
		return objectStateMessage(err)
	}
	h.send(ctx, b, chatID, "任务 #"+id(job.ID)+" 已取消。\n注意：正在发送中的提醒无法撤回已经发出的消息。")
	return "任务已取消"
}

func (h *Handler) settingsCallback(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, v2 callbackV2) string {
	switch v2.action {
	case "timezone":
		h.startTimezone(ctx, b, user, chatID)
		return "请输入或选择时区"
	case "help":
		h.send(ctx, b, chatID, helpText(user.Timezone))
		return "帮助已发送"
	case "view":
	default:
		return "未知操作"
	}
	h.sendMarkup(ctx, b, chatID, h.settingsText(ctx, user), markup(
		pair(button("修改时区", cb("settings", "timezone")), button("提醒任务", cb("job", "list"))),
		pair(button("服务列表", cb("service", "list")), button("帮助", cb("settings", "help"))),
	))
	return "设置已发送"
}

func objectStateMessage(err error) string {
	switch service.Code(err) {
	case "not_found":
		return "对象不存在或已删除"
	case "conflict":
		return "对象状态已变化，请重新打开详情"
	case "forbidden":
		return "无权限使用此机器人"
	}
	return serviceMessage(err)
}
