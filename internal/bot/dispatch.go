package bot

import (
	"context"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// startsWrite reports whether a command opens a flow that writes. Read-only
// views never disturb an unfinished wizard; a write flow must be confirmed
// explicitly before the draft is replaced.
func startsWrite(name string) bool {
	return name == "add"
}

// conflictTarget resumes only when both action and target match; a different
// object of the same action still requires the explicit replacement choice.
func (h *Handler) conflictTarget(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, action string, target int64, intent string) bool {
	d, err := h.svc.GetDialog(ctx, user.ID)
	if err != nil {
		return false
	}
	if d.ChatID != chatID {
		h.send(ctx, b, chatID, "当前流程已在其他会话中开始；请先发送 /cancel 取消。")
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
	w := h.wizard(ctx, b, user, d)
	if same {
		w.render(ctx)
		return true
	}
	w.promptReplaceStart(ctx, intent)
	return true
}

// dispatchCommand runs a slash command. fromReplace marks a command launched
// after the user explicitly confirmed discarding the previous draft.
func (h *Handler) dispatchCommand(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, text string, fromReplace bool) {
	name, target, rest, ok := splitCommand(text)
	if !ok {
		h.send(ctx, b, chatID, "未知命令。\n\n"+helpText(user.Timezone))
		return
	}
	if target != "" {
		known := h.Username()
		if known == "" || !strings.EqualFold(target, known) {
			return
		}
	}
	if !fromReplace && startsWrite(name) {
		if existing, err := h.svc.GetDialog(ctx, user.ID); err == nil {
			if existing.ChatID != chatID {
				h.send(ctx, b, chatID, "当前流程已在其他会话中开始；请先发送 /cancel 取消。")
				return
			}
			w := h.wizard(ctx, b, user, existing)
			w.promptReplaceStart(ctx, service.DialogServiceAdd)
			return
		}
	}
	switch name {
	case "start":
		h.ensureMenuButton(ctx, b, user, chatID)
		h.helpMessage(ctx, b, user, chatID)
	case "help":
		h.helpMessage(ctx, b, user, chatID)
	case "add":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, chatID, "用法：/add（直接进入新建服务向导）")
			return
		}
		h.startServiceAdd(ctx, b, user, chatID)
	case "services":
		query := strings.TrimSpace(rest)
		if len([]rune(query)) > 200 {
			h.send(ctx, b, chatID, "关键词最多 200 字。")
			return
		}
		h.showOverview(ctx, b, user.ID, chatID, service.ServiceFilter{Status: "active", Query: query}, 0)
	case "reminders":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, chatID, "用法：/reminders（在消息按钮中切换筛选）")
			return
		}
		h.showJobs(ctx, b, user, chatID, "pending", 0)
	case "settings":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, chatID, "用法：/settings")
			return
		}
		h.showSettings(ctx, b, user, chatID)
	case "cancel":
		if strings.TrimSpace(rest) != "" {
			h.send(ctx, b, chatID, "用法：/cancel（取消当前未完成流程）")
			return
		}
		removed, err := h.svc.CancelDialog(ctx, user.ID)
		if err != nil {
			h.send(ctx, b, chatID, serviceMessage(err))
			return
		}
		if removed {
			h.send(ctx, b, chatID, "已取消当前流程，未写入任何修改。\n\n"+shortHelp)
		} else {
			h.send(ctx, b, chatID, "当前没有进行中的对话。")
		}
	default:
		// Unknown commands, including the removed ID-based forms, are
		// read-only help responses.
		h.send(ctx, b, chatID, "未知命令。\n\n"+helpText(user.Timezone))
	}
}

func (h *Handler) helpMessage(ctx context.Context, b *telegram.Bot, user service.User, chatID int64) {
	rows := [][]models.InlineKeyboardButton{
		pair(button("我的服务", cb("service", "list", "1", "0", "0")), button("添加服务", cb("dialog", "new_service"))),
		pair(button("提醒任务", cb("job", "list", "1", "0")), button("设置", cb("settings", "view"))),
	}
	if d, err := h.svc.GetDialog(ctx, user.ID); err == nil && d.ChatID == chatID {
		rows = append(rows, pair(button("继续当前流程", cb("dialog", "resume"))))
	}
	h.sendMarkup(ctx, b, chatID, helpText(user.Timezone), markup(rows...))
}

// startIntent runs a wizard start encoded in a replacement prompt. The intent
// is code-generated, never taken from callback data.
func (h *Handler) startIntent(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, intent string) {
	parts := strings.Split(intent, ":")
	target := int64(0)
	if len(parts) > 1 {
		target, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	switch parts[0] {
	case service.DialogServiceAdd:
		h.startServiceAdd(ctx, b, user, chatID)
	case service.DialogTimezone:
		h.startTimezone(ctx, b, user, chatID)
	case service.DialogServiceEdit:
		h.startServiceEdit(ctx, b, user, chatID, target)
	case service.DialogPeriodAdd:
		h.startPeriodAdd(ctx, b, user, chatID, target)
	case service.DialogPeriodEdit:
		h.startPeriodEdit(ctx, b, user, chatID, target)
	case service.DialogRuleAdd:
		h.startRuleAdd(ctx, b, user, chatID, target)
	case service.DialogRuleEdit:
		h.startRuleEdit(ctx, b, user, chatID, target)
	case service.DialogRenew:
		h.startRenewWizard(ctx, b, user, chatID, target)
	default:
		h.send(ctx, b, chatID, "该流程已失效，请重新打开对应详情。")
	}
}
