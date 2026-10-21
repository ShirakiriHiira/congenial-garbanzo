package bot

import (
	"context"
	"fmt"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const expiredButtonMessage = "按钮已过期，请发送 /services 重新打开"

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
	v3, ok := parseCallbackV3(cq.Data)
	if !ok {
		if strings.HasPrefix(cq.Data, "v2:") {
			answer = expiredButtonMessage
		} else {
			answer = "未知操作"
		}
		return
	}
	answer = h.handleCallbackV3(ctx, b, cq, v3)
}

// callbackIdentity applies the transport and allowlist checks shared by all
// v3 payloads, then authenticates the sender's private chat.
func (h *Handler) callbackIdentity(ctx context.Context, cq *models.CallbackQuery) (service.User, models.Message, string) {
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

func (h *Handler) handleCallbackV3(ctx context.Context, b *telegram.Bot, cq *models.CallbackQuery, v3 callbackV3) string {
	user, msg, failure := h.callbackIdentity(ctx, cq)
	if failure != "" {
		return failure
	}
	switch v3.scope {
	case "dialog":
		return h.dialogCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "service":
		return h.serviceCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "period":
		return h.periodCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "rule":
		return h.ruleCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "job":
		return h.jobCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "settings":
		return h.settingsCallback(ctx, b, user, &responder{chatID: msg.Chat.ID, editID: msg.ID}, v3)
	case "notify":
		// Reminder cards are delivered messages the user may already have kept;
		// editing one would destroy the reminder body, so this responder never
		// carries an editID.
		return h.notifyCallback(ctx, b, user, &responder{chatID: msg.Chat.ID}, v3)
	default:
		return "未知操作"
	}
}

func (h *Handler) dialogCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	switch v3.action {
	case "cancel":
		return h.cancelDialog(ctx, b, user, resp, "当前没有进行中的流程")
	case "new_service":
		h.startServiceAdd(ctx, b, user, resp)
		return "请选择类别"
	case "resume", "confirm", "keep", "replace", "back", "skip", "skip_rest", "set", "change_field", "new_category", "use_convention", "clear_value", "toggle_enabled":
	default:
		return "未知操作"
	}
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		// The dialog this keyboard belongs to is gone, so the buttons must go
		// with it; the answer alone would leave a dead keyboard behind.
		message := "流程已过期，请重新打开详情"
		if service.Code(err) != "not_found" {
			message = serviceMessage(err)
		}
		h.send(ctx, b, resp, message)
		return message
	}
	if d.ChatID != resp.chatID {
		h.send(ctx, b, resp, otherChatMessage)
		return "当前流程已在其他会话中开始；请先发送 /cancel 取消"
	}
	w := h.wizard(ctx, b, user, d, resp)
	w.nav(ctx, v3.action, v3.args)
	switch v3.action {
	case "confirm":
		if _, err := h.svc.GetDialog(ctx, user.ID); err == nil {
			return "请按提示完成当前步骤"
		}
		return "已提交"
	case "replace":
		return "已切换流程"
	case "resume":
		return "已继续当前流程"
	default:
		return "已更新"
	}
}

func (h *Handler) serviceCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	switch v3.action {
	case "list":
		code, category, after := "1", int64(0), int64(0)
		if len(v3.args) > 0 {
			code = v3.args[0]
			if _, ok := serviceFilterFromCode(code); !ok {
				return "未知操作"
			}
		}
		if len(v3.args) > 1 {
			var ok bool
			if category, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		if len(v3.args) > 2 {
			var ok bool
			if after, ok = callbackID(v3.args, 2); !ok {
				return "未知操作"
			}
		}
		status, _ := serviceFilterFromCode(code)
		filter := service.ServiceFilter{Status: status}
		if category > 0 {
			filter.CategoryID = &category
		}
		h.showOverview(ctx, b, user.ID, resp, filter, after)
		return "服务列表已发送"
	case "cats":
		code := "1"
		if len(v3.args) > 0 {
			code = v3.args[0]
		}
		h.showCategories(ctx, b, user.ID, resp, code)
		return "请选择类别"
	}
	serviceID, ok := callbackID(v3.args, 0)
	if !ok {
		return "未知操作"
	}
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		after := int64(0)
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showServiceDetail(ctx, b, user, resp, serviceID, after)
		return "详情已发送"
	case "edit":
		h.startServiceEdit(ctx, b, user, resp, serviceID)
		return "请选择要修改的字段"
	case "archive_ask":
		if item.Status == "archived" {
			return "服务已归档"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("归档服务「%s」？\n归档会保留有效期、续期历史与已发送记录，停止未完成的提醒任务；之后可恢复。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认归档", cb("service", "archive", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return "请确认归档"
	case "archive":
		if item.Status != "archived" {
			status := "archived"
			if _, err := h.svc.UpdateService(ctx, user.ID, serviceID, service.ServicePatch{Status: &status}); err != nil {
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showServiceDetail(ctx, b, user, resp, serviceID, 0)
		return "服务已归档"
	case "restore_ask":
		if item.Status == "active" {
			return "服务已是可用状态"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("恢复服务「%s」？\n恢复后有效期会推进版本，旧的按钮与草稿失效；历史与已发送记录保留。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认恢复", cb("service", "restore", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return "请确认恢复"
	case "restore":
		if item.Status != "active" {
			status := "active"
			if _, err := h.svc.UpdateService(ctx, user.ID, serviceID, service.ServicePatch{Status: &status}); err != nil {
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showServiceDetail(ctx, b, user, resp, serviceID, 0)
		return "服务已恢复"
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除服务「%s」？\n将同时永久删除它的全部有效期、提醒规则、提醒任务与续期历史，无法恢复。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("service", "delete", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return "请确认删除"
	case "delete":
		if err := h.svc.DeleteService(ctx, user.ID, serviceID); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("服务「%s」已永久删除。", item.Name), markup([]models.InlineKeyboardButton{button("返回服务列表", cb("service", "list", "1", "0", "0"))}))
		return "服务已删除"
	default:
		return "未知操作"
	}
}

func (h *Handler) periodCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	targetID, ok := callbackID(v3.args, 0)
	if !ok {
		return "未知操作"
	}
	if v3.action == "add" {
		h.startPeriodAdd(ctx, b, user, resp, targetID)
		return "请填写有效期"
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, targetID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		h.showPeriodDetail(ctx, b, user, resp, p.ID)
		return "详情已发送"
	case "renew":
		h.startRenewWizard(ctx, b, user, resp, p.ID)
		return "请输入新的截止"
	case "rules":
		after := int64(0)
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showRules(ctx, b, user, resp, p.ID, after)
		return "规则已发送"
	case "renewals":
		after := int64(0)
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showRenewals(ctx, b, user, resp, p.ID, after)
		return "续期历史已发送"
	case "edit":
		h.startPeriodEdit(ctx, b, user, resp, p.ID)
		return "请选择要修改的字段"
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除有效期「%s」？\n将同时永久删除它的提醒规则、提醒任务与续期历史，无法恢复。", periodName(p)),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("period", "delete", id(p.ID))), button("取消", cb("period", "view", id(p.ID)))}))
		return "请确认删除"
	case "delete":
		if err := h.svc.DeletePeriod(ctx, user.ID, p.ID, p.Cycle); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("有效期「%s」已永久删除。", periodName(p)), markup([]models.InlineKeyboardButton{button("返回服务", cb("service", "view", id(p.ServiceID), "0"))}))
		return "有效期已删除"
	default:
		return "未知操作"
	}
}

func (h *Handler) ruleCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	targetID, ok := callbackID(v3.args, 0)
	if !ok {
		return "未知操作"
	}
	if v3.action == "add" {
		h.startRuleAdd(ctx, b, user, resp, targetID)
		return "请输入提前天数"
	}
	r, err := h.svc.GetRule(ctx, user.ID, targetID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		h.showRuleDetail(ctx, b, user, resp, r.ID)
		return "详情已发送"
	case "edit":
		h.startRuleEdit(ctx, b, user, resp, r.ID)
		return "请输入提前天数"
	case "enable_ask", "disable_ask":
		desired := v3.action == "enable_ask"
		if r.Enabled == desired {
			h.send(ctx, b, resp, ruleLinePlain(r)+"\n未重复修改。")
			return "规则状态未变化"
		}
		verb := "禁用"
		if desired {
			verb = "启用"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("%s这条规则？\n提前 %d 天 · %s\n%s后，未完成的相关任务会取消并重新排程。", verb, r.DaysBefore, r.ReminderTime, verb),
			markup([]models.InlineKeyboardButton{button("确认"+verb, cb("rule", v3.action[:len(v3.action)-4], id(r.ID))), button("取消", cb("rule", "view", id(r.ID)))}))
		return "请确认"
	case "enable", "disable":
		enabled := v3.action == "enable"
		if r.Enabled != enabled {
			if _, err := h.svc.UpdateRule(ctx, user.ID, r.ID, service.RulePatch{ExpectedRevision: r.Revision, Enabled: &enabled}); err != nil {
				if service.Code(err) == "conflict" {
					return "规则已被修改，请重新打开规则列表"
				}
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showRuleDetail(ctx, b, user, resp, r.ID)
		return "规则状态已更新"
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除规则（提前 %d 天 %s）？\n会同时删除该规则的任务记录，无法恢复。", r.DaysBefore, r.ReminderTime),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("rule", "delete", id(r.ID))), button("取消", cb("rule", "view", id(r.ID)))}))
		return "请确认删除"
	case "delete":
		if err := h.svc.DeleteRule(ctx, user.ID, r.ID, r.Revision); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, "提醒规则已永久删除。", markup([]models.InlineKeyboardButton{button("返回规则列表", cb("period", "rules", id(r.PeriodID), "0"))}))
		return "规则已删除"
	default:
		return "未知操作"
	}
}

func (h *Handler) jobCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	if v3.action == "list" {
		code, after := "1", int64(0)
		if len(v3.args) > 0 {
			code = v3.args[0]
		}
		filter, ok := jobFilterFromCode(code)
		if !ok {
			return "未知操作"
		}
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showJobs(ctx, b, user, resp, filter, after)
		return "提醒列表已发送"
	}
	jobID, ok := callbackID(v3.args, 0)
	if !ok {
		return "未知操作"
	}
	job, err := h.svc.GetJob(ctx, user.ID, jobID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "cancel_ask":
		if job.Status == "sent" {
			return "任务已发送，无法取消"
		}
		if job.Status == "cancelled" {
			return "任务已取消"
		}
		h.sendMarkup(ctx, b, resp, "取消该提醒任务？\n已发送的消息无法撤回；正在发送中的任务可能仍会完成一次投递。",
			markup([]models.InlineKeyboardButton{button("确认取消", cb("job", "cancel", id(job.ID))), button("返回提醒列表", cb("job", "list", "1", "0"))}))
		return "请确认取消"
	case "cancel":
		if err := h.svc.CancelJob(ctx, user.ID, job.ID); err != nil {
			if service.Code(err) == "conflict" {
				return "任务状态已变化（可能已发送）"
			}
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, "任务已取消。\n注意：正在发送中的提醒无法撤回已经发出的消息。", markup([]models.InlineKeyboardButton{button("返回提醒列表", cb("job", "list", "1", "0"))}))
		return "任务已取消"
	default:
		return "未知操作"
	}
}

func (h *Handler) settingsCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	switch v3.action {
	case "timezone":
		h.startTimezone(ctx, b, user, resp)
		return "请输入或选择时区"
	case "help":
		h.helpMessage(ctx, b, user, resp)
		return "帮助已发送"
	case "view":
	default:
		return "未知操作"
	}
	h.showSettings(ctx, b, user, resp)
	return "设置已发送"
}

// notifyCallback handles the buttons attached to a delivered reminder. The
// reminder body is history the user may still need, so every reply is a new
// message instead of an edit of the card.
func (h *Handler) notifyCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	targetID, ok := callbackID(v3.args, 0)
	if !ok {
		return "未知操作"
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, targetID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		h.showPeriodDetail(ctx, b, user, resp, p.ID)
		return "详情已发送"
	case "renew":
		h.startRenewWizard(ctx, b, user, resp, p.ID)
		return "请输入新的截止"
	default:
		return "未知操作"
	}
}

// replyState reports why a callback could not act on its object. It emits the
// message so an edit replaces the stale keyboard, then returns the same text for
// the callback answer.
func (h *Handler) replyState(ctx context.Context, b *telegram.Bot, resp *responder, message string) string {
	h.send(ctx, b, resp, message)
	return message
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
