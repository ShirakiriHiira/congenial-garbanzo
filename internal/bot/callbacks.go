package bot

import (
	"context"
	"fmt"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const expiredButtonMessage = "按钮已过期，请重新打开。"

// silentAnswer acknowledges a callback without a popup. Telegram still needs
// the answer to stop the button spinner, but the chat already shows the
// result, so a toast would only repeat it ("详情已发送" and friends).
const silentAnswer = ""

// handleCallback treats the callback payload as untrusted input. It answers
// every query exactly once, including unauthorized, stale, and malformed ones.
// The answer text is a toast: it is only set when the chat showed nothing, so
// handlers return silentAnswer once they emitted a message or an edit.
func (h *Handler) handleCallback(ctx context.Context, b *telegram.Bot, cq *models.CallbackQuery) {
	if cq == nil {
		return
	}
	answer := "操作失败"
	defer func() {
		params := &telegram.AnswerCallbackQueryParams{CallbackQueryID: cq.ID}
		if answer != silentAnswer {
			params.Text = answer
		}
		if _, err := b.AnswerCallbackQuery(ctx, params); err != nil {
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
		return service.User{}, models.Message{}, "请在私聊中使用。"
	}
	msg := cq.Message.Message
	if msg.Chat.Type != models.ChatTypePrivate || cq.From.ID <= 0 || msg.Chat.ID != cq.From.ID {
		return service.User{}, models.Message{}, "请在私聊中使用。"
	}
	uid := cq.From.ID
	if !h.svc.Allowed(uid) {
		return service.User{}, models.Message{}, "无权限使用此机器人。"
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
		h.cancelDialog(ctx, b, user, resp, "当前没有进行中的对话。")
		return silentAnswer
	case "new_service":
		h.startServiceAdd(ctx, b, user, resp)
		return silentAnswer
	case "resume", "confirm", "keep", "replace", "back", "skip", "skip_rest", "set", "change_field", "new_category", "use_convention", "clear_value", "toggle_enabled":
	default:
		return "未知操作"
	}
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		// The dialog this keyboard belongs to is gone, so the buttons must go
		// with it; the answer alone would leave a dead keyboard behind.
		message := "流程已过期，请重新打开。"
		if service.Code(err) != "not_found" {
			message = serviceMessage(err)
		}
		h.send(ctx, b, resp, message)
		return silentAnswer
	}
	if d.ChatID != resp.chatID {
		h.send(ctx, b, resp, otherChatMessage)
		return silentAnswer
	}
	w := h.wizard(ctx, b, user, d, resp)
	w.nav(ctx, v3.action, v3.args)
	// The wizard always answers in the chat, so the callback stays silent.
	return silentAnswer
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
		return silentAnswer
	case "cats":
		code := "1"
		if len(v3.args) > 0 {
			code = v3.args[0]
		}
		h.showCategories(ctx, b, user.ID, resp, code)
		return silentAnswer
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
		return silentAnswer
	case "edit":
		h.startServiceEdit(ctx, b, user, resp, serviceID)
		return silentAnswer
	case "archive_ask":
		if item.Status == "archived" {
			return "服务已归档"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("归档服务「%s」？\n未发送的提醒会取消，资料与历史保留，可随时恢复。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认归档", cb("service", "archive", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return silentAnswer
	case "archive":
		if item.Status != "archived" {
			status := "archived"
			if _, err := h.svc.UpdateService(ctx, user.ID, serviceID, service.ServicePatch{Status: &status}); err != nil {
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showServiceDetail(ctx, b, user, resp, serviceID, 0)
		return silentAnswer
	case "restore_ask":
		if item.Status == "active" {
			return "服务已是可用状态"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("恢复服务「%s」？\n未发送的提醒会取消，旧的按钮与草稿失效。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认恢复", cb("service", "restore", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return silentAnswer
	case "restore":
		if item.Status != "active" {
			status := "active"
			if _, err := h.svc.UpdateService(ctx, user.ID, serviceID, service.ServicePatch{Status: &status}); err != nil {
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showServiceDetail(ctx, b, user, resp, serviceID, 0)
		return silentAnswer
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除服务「%s」？\n有效期、提醒规则、提醒任务与续期记录会一并删除，无法恢复。", item.Name),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("service", "delete", id(item.ID))), button("取消", cb("service", "view", id(item.ID), "0"))}))
		return silentAnswer
	case "delete":
		if err := h.svc.DeleteService(ctx, user.ID, serviceID); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("服务「%s」已永久删除。", item.Name), markup([]models.InlineKeyboardButton{button("返回服务列表", cb("service", "list", "1", "0", "0"))}))
		return silentAnswer
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
		return silentAnswer
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, targetID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		h.showPeriodDetail(ctx, b, user, resp, p.ID)
		return silentAnswer
	case "renew":
		h.startRenewWizard(ctx, b, user, resp, p.ID)
		return silentAnswer
	case "rules":
		after := int64(0)
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showRules(ctx, b, user, resp, p.ID, after)
		return silentAnswer
	case "renewals":
		after := int64(0)
		if len(v3.args) > 1 {
			if after, ok = callbackID(v3.args, 1); !ok {
				return "未知操作"
			}
		}
		h.showRenewals(ctx, b, user, resp, p.ID, after)
		return silentAnswer
	case "edit":
		h.startPeriodEdit(ctx, b, user, resp, p.ID)
		return silentAnswer
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除有效期「%s」？\n提醒规则、提醒任务与续期记录会一并删除，无法恢复。", periodName(p)),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("period", "delete", id(p.ID))), button("取消", cb("period", "view", id(p.ID)))}))
		return silentAnswer
	case "delete":
		if err := h.svc.DeletePeriod(ctx, user.ID, p.ID, p.Cycle); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("有效期「%s」已永久删除。", periodName(p)), markup([]models.InlineKeyboardButton{button("返回服务", cb("service", "view", id(p.ServiceID), "0"))}))
		return silentAnswer
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
		return silentAnswer
	}
	r, err := h.svc.GetRule(ctx, user.ID, targetID)
	if err != nil {
		return h.replyState(ctx, b, resp, objectStateMessage(err))
	}
	switch v3.action {
	case "view":
		h.showRuleDetail(ctx, b, user, resp, r.ID)
		return silentAnswer
	case "edit":
		h.startRuleEdit(ctx, b, user, resp, r.ID)
		return silentAnswer
	case "enable_ask", "disable_ask":
		desired := v3.action == "enable_ask"
		if r.Enabled == desired {
			h.send(ctx, b, resp, ruleLinePlain(r)+"\n未做修改。")
			return silentAnswer
		}
		verb := "禁用"
		if desired {
			verb = "启用"
		}
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("%s这条规则？\n提前 %d 天 · %s\n%s", verb, r.DaysBefore, r.ReminderTime, ruleToggleEffect(desired)),
			markup([]models.InlineKeyboardButton{button("确认"+verb, cb("rule", v3.action[:len(v3.action)-4], id(r.ID))), button("取消", cb("rule", "view", id(r.ID)))}))
		return silentAnswer
	case "enable", "disable":
		enabled := v3.action == "enable"
		if r.Enabled != enabled {
			if _, err := h.svc.UpdateRule(ctx, user.ID, r.ID, service.RulePatch{ExpectedRevision: r.Revision, Enabled: &enabled}); err != nil {
				if service.Code(err) == "conflict" {
					return "规则已修改，请重新打开。"
				}
				return h.replyState(ctx, b, resp, objectStateMessage(err))
			}
		}
		h.showRuleDetail(ctx, b, user, resp, r.ID)
		return silentAnswer
	case "delete_ask":
		h.sendMarkup(ctx, b, resp, fmt.Sprintf("永久删除规则（提前 %d 天 %s）？\n任务记录会一并删除，无法恢复。", r.DaysBefore, r.ReminderTime),
			markup([]models.InlineKeyboardButton{button("确认永久删除", cb("rule", "delete", id(r.ID))), button("取消", cb("rule", "view", id(r.ID)))}))
		return silentAnswer
	case "delete":
		if err := h.svc.DeleteRule(ctx, user.ID, r.ID, r.Revision); err != nil {
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, "提醒规则已永久删除。", markup([]models.InlineKeyboardButton{button("返回规则列表", cb("period", "rules", id(r.PeriodID), "0"))}))
		return silentAnswer
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
		return silentAnswer
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
		h.sendMarkup(ctx, b, resp, "取消该提醒任务？\n发送中的提醒可能仍会送达。",
			markup([]models.InlineKeyboardButton{button("确认取消", cb("job", "cancel", id(job.ID))), button("返回提醒列表", cb("job", "list", "1", "0"))}))
		return silentAnswer
	case "cancel":
		if err := h.svc.CancelJob(ctx, user.ID, job.ID); err != nil {
			if service.Code(err) == "conflict" {
				return "任务状态已变化。"
			}
			return h.replyState(ctx, b, resp, objectStateMessage(err))
		}
		h.sendMarkup(ctx, b, resp, "任务已取消。", markup([]models.InlineKeyboardButton{button("返回提醒列表", cb("job", "list", "1", "0"))}))
		return silentAnswer
	default:
		return "未知操作"
	}
}

func (h *Handler) settingsCallback(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, v3 callbackV3) string {
	switch v3.action {
	case "timezone":
		h.startTimezone(ctx, b, user, resp)
		return silentAnswer
	case "help":
		h.helpMessage(ctx, b, user, resp)
		return silentAnswer
	case "view":
	default:
		return "未知操作"
	}
	h.showSettings(ctx, b, user, resp)
	return silentAnswer
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
		return silentAnswer
	case "renew":
		h.startRenewWizard(ctx, b, user, resp, p.ID)
		return silentAnswer
	default:
		return "未知操作"
	}
}

// replyState reports why a callback could not act on its object. It emits the
// message so an edit replaces the stale keyboard; the chat already shows the
// reason, so the callback answer stays silent.
func (h *Handler) replyState(ctx context.Context, b *telegram.Bot, resp *responder, message string) string {
	h.send(ctx, b, resp, message)
	return silentAnswer
}

func objectStateMessage(err error) string {
	switch service.Code(err) {
	case "not_found":
		return "内容不存在或已删除"
	case "conflict":
		return "状态已变化，请重新打开详情"
	case "forbidden":
		return "无权限使用此机器人。"
	}
	return serviceMessage(err)
}

// ruleToggleEffect states what enabling or disabling a rule does to the tasks
// that have not been sent yet.
func ruleToggleEffect(enabled bool) string {
	if enabled {
		return "未发送的提醒会重新排程。"
	}
	return "未发送的提醒会取消。"
}
