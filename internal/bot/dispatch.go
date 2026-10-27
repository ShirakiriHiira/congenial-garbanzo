package bot

import (
	"context"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// conflictTarget resumes only when both action and target match; a different
// object of the same action still requires the explicit replacement choice.
func (h *Handler) conflictTarget(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, action string, target int64, intent string) bool {
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		return false
	}
	if d.ChatID != resp.chatID {
		h.send(ctx, b, resp, otherChatMessage)
		return true
	}
	same := d.Action == action
	if same && target > 0 {
		switch action {
		case service.DialogServiceEdit, service.DialogPeriodAdd:
			same = d.ServiceID != nil && *d.ServiceID == target
		case service.DialogPeriodEdit, service.DialogRuleAdd, service.DialogRenew:
			same = d.PeriodID != nil && *d.PeriodID == target
		case service.DialogRuleEdit:
			same = d.RuleID != nil && *d.RuleID == target
		}
	}
	w := h.wizard(ctx, b, user, d, resp)
	if same {
		w.render(ctx)
		return true
	}
	w.promptReplaceStart(ctx, intent)
	return true
}

// dispatchCommand runs a slash command. fromReplace marks a command launched
// after the user explicitly confirmed discarding the previous draft.
func (h *Handler) dispatchCommand(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, text string, fromReplace bool) {
	name, target, rest, ok := splitCommand(text)
	if !ok {
		h.send(ctx, b, resp, "未知命令，发送 /help 查看。")
		return
	}
	if target != "" {
		known := h.Username()
		if known == "" || !strings.EqualFold(target, known) {
			return
		}
	}
	if !fromReplace && name == "add" {
		if existing, err := h.svc.GetDialog(ctx, user.ID); err == nil {
			if existing.ChatID != resp.chatID {
				h.send(ctx, b, resp, otherChatMessage)
				return
			}
			w := h.wizard(ctx, b, user, existing, resp)
			w.promptReplaceStart(ctx, service.DialogServiceAdd)
			return
		}
	}
	switch name {
	case "start":
		h.ensureMenuButton(ctx, b, user, resp.chatID)
		h.helpMessage(ctx, b, user, resp)
	case "help":
		h.helpMessage(ctx, b, user, resp)
	case "add":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, resp, "用法：/add")
			return
		}
		h.startServiceAdd(ctx, b, user, resp)
	case "services":
		query := strings.TrimSpace(rest)
		if len([]rune(query)) > 200 {
			h.send(ctx, b, resp, "关键词最多 200 字。")
			return
		}
		h.showOverview(ctx, b, user.ID, resp, service.ServiceFilter{Status: "active", Query: query}, 0)
	case "reminders":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, resp, "用法：/reminders")
			return
		}
		h.showJobs(ctx, b, user, resp, "pending", 0)
	case "settings":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, resp, "用法：/settings")
			return
		}
		h.showSettings(ctx, b, user, resp)
	case "cancel":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, resp, "用法：/cancel")
			return
		}
		h.cancelDialog(ctx, b, user, resp, "当前没有进行中的对话。")
	default:
		// Unknown commands, including the removed ID-based forms, are
		// read-only help responses.
		h.send(ctx, b, resp, "未知命令，发送 /help 查看。")
	}
}

func (h *Handler) helpMessage(ctx context.Context, b *telegram.Bot, user service.User, resp *responder) {
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("我的服务", cb("service", "list", "1", "0", "0")), button("添加服务", cb("dialog", "new_service"))},
		[]models.InlineKeyboardButton{button("提醒任务", cb("job", "list", "1", "0")), button("设置", cb("settings", "view"))},
	}
	if d, err := h.svc.GetDialog(ctx, user.ID); err == nil && d.ChatID == resp.chatID {
		rows = append(rows, []models.InlineKeyboardButton{button("继续当前流程", cb("dialog", "resume"))})
	}
	h.sendMarkup(ctx, b, resp, helpText(), markup(rows...))
}

// startIntent runs a wizard start encoded in a replacement prompt. The intent
// is code-generated, never taken from callback data.
func (h *Handler) startIntent(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, intent string) {
	parts := strings.Split(intent, ":")
	target := int64(0)
	if len(parts) > 1 {
		target, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	switch parts[0] {
	case service.DialogServiceAdd:
		h.startServiceAdd(ctx, b, user, resp)
	case service.DialogTimezone:
		h.startTimezone(ctx, b, user, resp)
	case service.DialogServiceEdit:
		h.startServiceEdit(ctx, b, user, resp, target)
	case service.DialogPeriodAdd:
		h.startPeriodAdd(ctx, b, user, resp, target)
	case service.DialogPeriodEdit:
		h.startPeriodEdit(ctx, b, user, resp, target)
	case service.DialogRuleAdd:
		h.startRuleAdd(ctx, b, user, resp, target)
	case service.DialogRuleEdit:
		h.startRuleEdit(ctx, b, user, resp, target)
	case service.DialogRenew:
		h.startRenewWizard(ctx, b, user, resp, target)
	default:
		h.send(ctx, b, resp, expiredButtonMessage)
	}
}
