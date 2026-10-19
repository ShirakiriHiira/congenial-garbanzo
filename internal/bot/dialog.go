package bot

import (
	"context"
	"encoding/json"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// draft is the persisted, per-user wizard state. Values holds only fields the
// user supplied; an empty string is a deliberate clear for edit flows. The
// dict is bounded by service.validDialogDraft and is never logged.
type draft struct {
	Values  map[string]string `json:"v,omitempty"`
	Pending string            `json:"pending,omitempty"`
	Resume  string            `json:"resume,omitempty"`
}

func parseDraft(raw string) draft {
	d := draft{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &d)
	}
	if d.Values == nil {
		d.Values = map[string]string{}
	}
	return d
}

func (d draft) encode() string {
	b, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (d draft) get(key string) (string, bool) {
	v, ok := d.Values[key]
	return v, ok
}

func (d draft) set(key, value string) { d.Values[key] = value }
func (d draft) del(key string)        { delete(d.Values, key) }
func (d draft) has(key string) bool   { _, ok := d.Values[key]; return ok }

// wizard is one rendering/dispatch cycle for the caller's live dialog.
type wizard struct {
	h      *Handler
	b      *telegram.Bot
	user   service.User
	chatID int64
	d      service.Dialog
	dr     draft
}

func (h *Handler) wizard(ctx context.Context, b *telegram.Bot, user service.User, d service.Dialog) wizard {
	return wizard{h: h, b: b, user: user, chatID: d.ChatID, d: d, dr: parseDraft(d.Draft)}
}

// persist writes the draft (and optionally a new step) without extending the
// dialog TTL.
func (w *wizard) persist(ctx context.Context, step string) bool {
	if step != "" {
		w.d.Step = step
	}
	d, err := w.h.svc.UpdateDialogDraft(ctx, w.user.ID, w.d.Step, w.dr.encode())
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "保存对话进度失败："+serviceMessage(err)+"\n发送 /cancel 可取消当前流程。")
		return false
	}
	w.d = d
	return true
}

func (w *wizard) reply(ctx context.Context, text string, kbRows ...[]models.InlineKeyboardButton) {
	w.h.sendMarkup(ctx, w.b, w.chatID, text, markup(kbRows...))
}

func (w *wizard) failure(ctx context.Context, text string, canBack, canSkip bool) {
	w.reply(ctx, text, navRows(canBack, canSkip)...)
}

func (w *wizard) cancel(ctx context.Context) {
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.send(ctx, w.b, w.chatID, serviceMessage(err))
		return
	}
	w.h.send(ctx, w.b, w.chatID, "已取消当前流程，未写入任何修改。\n\n"+shortHelp)
}

func (w *wizard) skip(ctx context.Context) {
	if !optionalStep(w.d.Step) {
		w.failure(ctx, "当前步骤不能跳过。", true, false)
		return
	}
	step := nextStep(w.d.Action, w.dr, w.d.Step)
	if step == "" {
		w.failure(ctx, "当前步骤不能跳过。", true, false)
		return
	}
	if !w.persist(ctx, step) {
		return
	}
	w.render(ctx)
}

// skipRest jumps from any optional step of the service-add flow to the final
// preview. Other flows do not expose this shortcut.
func (w *wizard) skipRest(ctx context.Context) {
	if w.d.Action != service.DialogServiceAdd || !optionalStep(w.d.Step) {
		w.failure(ctx, "当前步骤不能跳过剩余资料。", true, false)
		return
	}
	if !w.persist(ctx, "add_preview") {
		return
	}
	w.render(ctx)
}

// stepError keeps the user on the current step with navigation intact.
func (w *wizard) stepError(ctx context.Context, format string, canBack, canSkip bool) {
	w.failure(ctx, format+"\n\n当前步骤未保存，可重新发送。", canBack, canSkip)
}

const replaceStep = "confirm_replace"

func (w *wizard) back(ctx context.Context) {
	if w.d.Action == service.DialogServiceAdd && w.d.Step == "add_category_name" {
		if !w.persist(ctx, "add_category") {
			return
		}
		w.render(ctx)
		return
	}
	prev := previousStep(w.d.Action, w.dr, w.d.Step)
	if prev == "" {
		w.failure(ctx, "已经在第一步。发送 /cancel 可取消当前流程。", false, false)
		return
	}
	if !w.persist(ctx, prev) {
		return
	}
	w.render(ctx)
}
func (w *wizard) promptReplace(ctx context.Context, command string) {
	if command != "" {
		w.dr.Pending = command
		if w.d.Step != replaceStep {
			w.dr.Resume = w.d.Step
		}
		if !w.persist(ctx, replaceStep) {
			return
		}
	}
	w.reply(ctx, "当前有未完成的流程："+describeTarget(ctx, w.h, w.user, w.d)+"\n\n继续当前流程不会丢失草稿；取消并开始会丢弃该草稿。\n发送 /cancel 也可取消当前流程。",
		pair(button("继续当前", cb("dialog", "keep")), button("取消并开始", cb("dialog", "replace"))))
}

// promptReplaceStart records a code-generated start intent for the explicit
// "cancel and start" button.
func (w *wizard) promptReplaceStart(ctx context.Context, intent string) {
	command := ""
	if intent != "" {
		command = "@start:" + intent
	}
	w.promptReplace(ctx, command)
}

func (w *wizard) keep(ctx context.Context) {
	if w.dr.Resume != "" {
		w.d.Step = w.dr.Resume
	}
	w.dr.Pending, w.dr.Resume = "", ""
	if !w.persist(ctx, w.d.Step) {
		return
	}
	w.render(ctx)
}

func (w *wizard) replace(ctx context.Context) {
	command := w.dr.Pending
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.send(ctx, w.b, w.chatID, serviceMessage(err))
		return
	}
	switch {
	case command == "":
		w.h.send(ctx, w.b, w.chatID, "已取消当前流程。\n\n"+shortHelp)
	case strings.HasPrefix(command, "@start:"):
		w.h.startIntent(ctx, w.b, w.user, w.chatID, strings.TrimPrefix(command, "@start:"))
	default:
		w.h.dispatchCommand(ctx, w.b, w.user, w.chatID, command, true)
	}
}

// render redraws the current step. A vanished or changed object closes the
// flow with an explicit message instead of writing stale data.
func (w *wizard) render(ctx context.Context) {
	switch w.d.Action {
	case service.DialogRenew:
		w.renderRenew(ctx)
	case service.DialogServiceAdd:
		w.renderServiceAdd(ctx)
	case service.DialogServiceEdit:
		w.renderServiceEdit(ctx)
	case service.DialogPeriodAdd:
		w.renderPeriodAdd(ctx)
	case service.DialogPeriodEdit:
		w.renderPeriodEdit(ctx)
	case service.DialogRuleAdd, service.DialogRuleEdit:
		w.renderRule(ctx)
	case service.DialogTimezone:
		w.renderTimezone(ctx)
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func (w *wizard) abort(ctx context.Context, reason string) {
	_, _ = w.h.svc.CancelDialog(ctx, w.user.ID)
	w.h.send(ctx, w.b, w.chatID, reason+"，请重新打开对应详情后再操作。")
}

func (w *wizard) text(ctx context.Context, text string) {
	if w.d.Step == replaceStep {
		w.promptReplace(ctx, text)
		return
	}
	switch w.d.Action {
	case service.DialogRenew:
		w.textRenew(ctx, text)
	case service.DialogServiceAdd:
		w.textServiceAdd(ctx, text)
	case service.DialogServiceEdit:
		w.textServiceEdit(ctx, text)
	case service.DialogPeriodAdd:
		w.textPeriodAdd(ctx, text)
	case service.DialogPeriodEdit:
		w.textPeriodEdit(ctx, text)
	case service.DialogRuleAdd, service.DialogRuleEdit:
		w.textRule(ctx, text)
	case service.DialogTimezone:
		w.textTimezone(ctx, text)
	default:
		w.abort(ctx, "该流程已失效")
	}
}

// nav handles v3:dialog:<action>[:arg...] callbacks.
func (w *wizard) nav(ctx context.Context, action string, args []string) {
	if action == "cancel" {
		w.cancel(ctx)
		return
	}
	if action == "resume" {
		w.render(ctx)
		return
	}
	if w.d.Step == replaceStep {
		switch action {
		case "keep":
			w.keep(ctx)
		case "replace":
			w.replace(ctx)
		default:
			w.failure(ctx, "请选择继续当前流程或取消并开始。", false, false)
		}
		return
	}
	switch action {
	case "back":
		w.back(ctx)
	case "skip":
		w.skip(ctx)
	case "skip_rest":
		w.skipRest(ctx)
	case "confirm":
		w.confirm(ctx)
	case "change_field":
		if w.d.Action != service.DialogServiceEdit && w.d.Action != service.DialogPeriodEdit {
			w.failure(ctx, "未知操作，请重新打开流程。", false, false)
			return
		}
		if !w.persist(ctx, "edit_field") {
			return
		}
		w.render(ctx)
	case "new_service":
		if w.d.Action != service.DialogServiceAdd {
			w.failure(ctx, "未知操作，请重新打开流程。", false, false)
			return
		}
		w.h.startServiceAdd(ctx, w.b, w.user, w.chatID)
	case "new_category":
		if w.d.Action != service.DialogServiceAdd || w.d.Step != "add_category" {
			w.failure(ctx, "未知操作，请重新打开流程。", false, false)
			return
		}
		if !w.persist(ctx, "add_category_name") {
			return
		}
		w.render(ctx)
	case "use_convention":
		w.useConvention(ctx)
	case "clear_value":
		w.clearValue(ctx)
	case "toggle_enabled":
		w.toggleEnabled(ctx)
	case "set":
		if len(args) != 2 {
			w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
			return
		}
		w.setChoice(ctx, args[0], args[1])
	default:
		w.failure(ctx, "未知操作，请重新打开流程。", false, false)
	}
}

// toggleEnabled flips the pending rule's enabled state at the preview step.
// The step gate is explicit so a crafted callback cannot change other flows.
func (w *wizard) toggleEnabled(ctx context.Context) {
	if (w.d.Action != service.DialogRuleAdd && w.d.Action != service.DialogRuleEdit) || w.d.Step != "rule_preview" {
		w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
		return
	}
	if w.dr.Values["enabled"] == "false" {
		w.dr.set("enabled", "true")
	} else {
		w.dr.set("enabled", "false")
	}
	if !w.persist(ctx, w.d.Step) {
		return
	}
	w.render(ctx)
}

func navRows(canBack, canSkip bool) [][]models.InlineKeyboardButton {
	row := []models.InlineKeyboardButton{}
	if canBack {
		row = append(row, button("上一步", cb("dialog", "back")))
	}
	if canSkip {
		row = append(row, button("跳过", cb("dialog", "skip")))
	}
	row = append(row, button("取消", cb("dialog", "cancel")))
	return [][]models.InlineKeyboardButton{row}
}

func optionalNav(canBack bool) [][]models.InlineKeyboardButton {
	return [][]models.InlineKeyboardButton{
		pair(button("上一步", cb("dialog", "back")), button("跳过", cb("dialog", "skip")), button("跳过剩余可选资料", cb("dialog", "skip_rest")), button("取消", cb("dialog", "cancel"))),
	}
}

func (w *wizard) clearValue(ctx context.Context) {
	if w.d.Step != "edit_value" || (w.d.Action != service.DialogServiceEdit && w.d.Action != service.DialogPeriodEdit) {
		w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
		return
	}
	if w.d.Action == service.DialogServiceEdit && w.dr.Values["field"] == "name" {
		w.failure(ctx, "名称不能清空。", true, false)
		return
	}
	w.dr.set("value", "")
	if !w.persist(ctx, "edit_preview") {
		return
	}
	w.render(ctx)
}

func trimmed(value string) string { return strings.TrimSpace(value) }

func describeTarget(ctx context.Context, h *Handler, user service.User, d service.Dialog) string {
	name := map[string]string{
		service.DialogRenew:       "续期",
		service.DialogServiceAdd:  "添加服务",
		service.DialogServiceEdit: "编辑服务",
		service.DialogPeriodAdd:   "添加有效期",
		service.DialogPeriodEdit:  "编辑有效期",
		service.DialogRuleAdd:     "添加提醒规则",
		service.DialogRuleEdit:    "编辑提醒规则",
		service.DialogTimezone:    "修改时区",
	}[d.Action]
	if name == "" {
		name = "未完成流程"
	}
	if d.ServiceID != nil {
		if item, err := h.svc.GetService(ctx, user.ID, *d.ServiceID); err == nil {
			return name + "（" + item.Name + "）"
		}
	}
	if d.PeriodID != nil {
		if p, err := h.svc.GetPeriod(ctx, user.ID, *d.PeriodID); err == nil {
			return name + "（" + periodName(p) + "）"
		}
	}
	return name
}

// confirm finalizes the preview step of the current wizard.
func (w *wizard) confirm(ctx context.Context) {
	if !strings.HasSuffix(w.d.Step, "_preview") {
		w.failure(ctx, "当前步骤还不能提交，请按提示继续。", true, false)
		return
	}
	switch w.d.Action {
	case service.DialogRenew:
		w.confirmRenew(ctx)
	case service.DialogServiceAdd:
		w.confirmServiceAdd(ctx)
	case service.DialogServiceEdit:
		w.confirmServiceEdit(ctx)
	case service.DialogPeriodAdd:
		w.confirmPeriodAdd(ctx)
	case service.DialogPeriodEdit:
		w.confirmPeriodEdit(ctx)
	case service.DialogRuleAdd, service.DialogRuleEdit:
		w.confirmRule(ctx)
	case service.DialogTimezone:
		w.confirmTimezone(ctx)
	default:
		w.abort(ctx, "该流程已失效")
	}
}
