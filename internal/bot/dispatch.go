package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// conflict reports whether a live wizard blocks a new write flow. When a
// different action is live, the user must choose explicitly; the draft is
// never discarded silently.
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
		case service.DialogTermsEdit, service.DialogRuleAdd, service.DialogRenew:
			same = d.ValidityPeriodID != nil && *d.ValidityPeriodID == target
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

func readOnlyCommand(name, rest string) bool {
	switch name {
	case "start", "help", "list", "view", "reminders", "settings":
		return true
	case "cancel":
		return strings.TrimSpace(rest) == ""
	case "service":
		args := strings.Fields(rest)
		if len(args) == 0 {
			return true
		}
		switch args[0] {
		case "list", "view", "categories":
			return true
		case "terms":
			// A terms query without arguments opens the terms wizard; with
			// arguments it is the advanced immediate update.
			return len(args) > 1
		}
		return false
	}
	return false
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
	if !fromReplace && !readOnlyCommand(name, rest) {
		existing, err := h.svc.GetDialog(ctx, user.ID)
		if err == nil {
			if existing.ChatID != chatID {
				h.send(ctx, b, chatID, "当前流程已在其他会话中开始；请先发送 /cancel 取消。")
				return
			}
			w := h.wizard(ctx, b, user, existing)
			w.promptReplace(ctx, text)
			return
		}
	}
	switch name {
	case "start", "help":
		h.send(ctx, b, chatID, helpText(user.Timezone))
	case "service":
		h.commandServiceDispatch(ctx, b, user, chatID, rest)
	case "add":
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandAdd(ctx, user, rest) })
	case "list":
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandList(ctx, user, rest) })
	case "view":
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandView(ctx, user, rest) })
	case "edit":
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandEdit(ctx, user, rest) })
	case "delete":
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandDelete(ctx, user, rest) })
	case "reminders":
		h.remindersCommand(ctx, b, user, chatID, rest)
	case "renew":
		h.commandRenewDispatch(ctx, b, user, chatID, rest)
	case "cancel":
		if strings.TrimSpace(rest) == "" {
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
			return
		}
		h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandCancel(ctx, user, rest) })
	case "settings":
		h.sendMarkup(ctx, b, chatID, h.settingsText(ctx, user), markup(
			pair(button("修改时区", cb("settings", "timezone")), button("提醒任务", cb("job", "list"))),
			pair(button("服务列表", cb("service", "list")), button("帮助", cb("settings", "help"))),
		))
	default:
		h.send(ctx, b, chatID, "未知命令。\n\n"+helpText(user.Timezone))
	}
}

// runTextCommand executes a read-only command; no dialog is touched.
func (h *Handler) runTextCommand(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, run func() (string, error)) {
	out, err := run()
	if err != nil {
		var ue userError
		if errors.As(err, &ue) {
			out = ue.msg
		} else {
			out = serviceMessage(err)
		}
	}
	h.send(ctx, b, chatID, out)
}

func (h *Handler) commandRenewDispatch(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, rest string) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		h.send(ctx, b, chatID, "用法：/renew <period_id> 打开续期向导；或 /renew <period_id> <query> 高级立即提交。\n请先使用 /service list 查看服务。")
		return
	}
	periodID, err := parseID(fields[0])
	if err != nil {
		h.send(ctx, b, chatID, "有效期 ID 必须是正整数。")
		return
	}
	if len(fields) == 1 {
		h.startRenewWizard(ctx, b, user, chatID, periodID)
		return
	}
	h.runTextCommand(ctx, b, user, chatID, func() (string, error) {
		return h.commandRenew(ctx, user, rest)
	})
}

// commandServiceDispatch keeps every legacy query form while adding
// interactive forms when the documented query is absent.
func (h *Handler) commandServiceDispatch(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, rest string) {
	args := strings.Fields(rest)
	if len(args) == 0 {
		h.showServiceMenu(ctx, b, chatID)
		return
	}
	switch args[0] {
	case "add":
		if len(args) == 1 {
			h.startServiceAdd(ctx, b, user, chatID)
			return
		}
	case "list":
		if len(args) == 1 {
			text, page, err := h.serviceListPage(ctx, user.ID, 0)
			if err != nil {
				h.send(ctx, b, chatID, serviceMessage(err))
				return
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
			return
		}
	case "view":
		if len(args) == 2 {
			objectID, err := parseID(args[1])
			if err != nil {
				h.send(ctx, b, chatID, "服务 ID 必须是正整数。")
				return
			}
			item, err := h.svc.GetService(ctx, user.ID, objectID)
			if err != nil {
				h.send(ctx, b, chatID, serviceMessage(err))
				return
			}
			text, page, err := h.serviceDetailPage(ctx, user.ID, item, 0)
			if err != nil {
				h.send(ctx, b, chatID, serviceMessage(err))
				return
			}
			h.sendMarkup(ctx, b, chatID, text, h.serviceKeyboard(item, &page))
			return
		}
	case "edit":
		if len(args) == 2 {
			objectID, err := parseID(args[1])
			if err != nil {
				h.send(ctx, b, chatID, "服务 ID 必须是正整数。")
				return
			}
			h.startServiceEdit(ctx, b, user, chatID, objectID)
			return
		}
	case "archive":
		if len(args) == 2 {
			objectID, err := parseID(args[1])
			if err != nil {
				h.send(ctx, b, chatID, "服务 ID 必须是正整数。")
				return
			}
			item, err := h.svc.GetService(ctx, user.ID, objectID)
			if err != nil {
				h.send(ctx, b, chatID, serviceMessage(err))
				return
			}
			if item.Status == "archived" {
				h.send(ctx, b, chatID, "服务 #"+id(item.ID)+" 已经是归档状态。")
				return
			}
			h.sendMarkup(ctx, b, chatID, "归档服务 #"+id(item.ID)+" "+item.Name+"？\n归档会保留有效期、续期历史与已发送记录，停止未完成的提醒任务；之后可恢复。",
				markup(pair(button("确认归档", cb("service", "archive", id(item.ID))), button("取消", cb("service", "view", id(item.ID))))))
			return
		}
	case "period":
		if len(args) == 3 && args[1] == "add" {
			objectID, err := parseID(args[2])
			if err != nil {
				h.send(ctx, b, chatID, "服务 ID 必须是正整数。")
				return
			}
			h.startPeriodAdd(ctx, b, user, chatID, objectID)
			return
		}
		if len(args) > 1 && args[1] == "add" {
			h.send(ctx, b, chatID, "用法：/service period add <service_id>")
			return
		}
	case "terms":
		if len(args) == 2 {
			objectID, err := parseID(args[1])
			if err != nil {
				h.send(ctx, b, chatID, "有效期 ID 必须是正整数。")
				return
			}
			h.startTermsEdit(ctx, b, user, chatID, objectID)
			return
		}
	case "rule":
		if len(args) == 3 && args[1] == "add" {
			objectID, err := parseID(args[2])
			if err != nil {
				h.send(ctx, b, chatID, "有效期 ID 必须是正整数。")
				return
			}
			h.startRuleAdd(ctx, b, user, chatID, objectID)
			return
		}
		if len(args) > 1 && args[1] == "add" {
			h.send(ctx, b, chatID, "用法：/service rule add <period_id>")
			return
		}
	}
	h.runTextCommand(ctx, b, user, chatID, func() (string, error) { return h.commandService(ctx, user, rest) })
}

func (h *Handler) showServiceMenu(ctx context.Context, b *telegram.Bot, chatID int64) {
	h.sendMarkup(ctx, b, chatID, "服务与订阅：\n使用按钮浏览与管理，或使用 /help 查看高级命令。", markup(
		pair(button("服务列表", cb("service", "list")), button("添加服务", cb("dialog", "new_service"))),
		pair(button("提醒任务", cb("job", "list")), button("设置", cb("settings", "view"))),
	))
}

// remindersCommand renders reminder jobs with per-task cancel buttons for the
// cancellable states only.
func (h *Handler) remindersCommand(ctx context.Context, b *telegram.Bot, user service.User, chatID int64, raw string) {
	args := strings.Fields(raw)
	if len(args) > 2 {
		h.send(ctx, b, chatID, "用法：/reminders [pending|sending|sent|failed|cancelled|all] [after_id]")
		return
	}
	filter := "all"
	after := int64(0)
	rest := args
	if len(rest) > 0 {
		switch rest[0] {
		case "pending", "sending", "sent", "failed", "cancelled", "all":
			filter = rest[0]
			rest = rest[1:]
		}
	}
	if len(rest) > 0 {
		var err error
		if after, err = parseAfter(rest[0]); err != nil {
			h.send(ctx, b, chatID, "after_id 必须是非负整数。")
			return
		}
	}
	text, page, err := h.remindersPage(ctx, user, filter, after)
	if err != nil {
		h.send(ctx, b, chatID, serviceMessage(err))
		return
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
	rows = append(rows, pair(button("设置", cb("settings", "view")), button("服务列表", cb("service", "list"))))
	h.sendMarkup(ctx, b, chatID, text, markup(rows...))
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
	case service.DialogTermsEdit:
		h.startTermsEdit(ctx, b, user, chatID, target)
	case service.DialogRuleAdd:
		h.startRuleAdd(ctx, b, user, chatID, target)
	case service.DialogRuleEdit:
		h.startRuleEdit(ctx, b, user, chatID, target)
	case service.DialogRenew:
		d, err := h.svc.StartPeriodRenewDialog(ctx, user.ID, target, chatID)
		if err != nil {
			h.send(ctx, b, chatID, "无法开始续期："+serviceMessage(err))
			return
		}
		w := h.wizard(ctx, b, user, d)
		w.renderRenew(ctx)
	default:
		h.send(ctx, b, chatID, "该流程已失效，请重新打开对应详情。")
	}
}
