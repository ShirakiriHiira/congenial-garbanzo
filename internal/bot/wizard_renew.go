package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"telegram.bot.remind/internal/service"
)

// startRenewWizard opens the renewal flow for one period. An existing
// different wizard is never discarded silently.
func (h *Handler) startRenewWizard(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRenew, periodID, service.DialogRenew+":"+id(periodID)) {
		return
	}
	d, err := h.svc.StartPeriodRenewDialog(ctx, user.ID, periodID, chatID)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始续期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, d)
	w.renderRenew(ctx)
}

func (w *wizard) renderRenew(ctx context.Context) {
	periodID := 0
	if w.d.PeriodID != nil {
		periodID = int(*w.d.PeriodID)
	}
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, int64(periodID))
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	item, err := w.h.svc.GetService(ctx, w.user.ID, p.ServiceID)
	if err != nil {
		w.abort(ctx, "服务已不存在")
		return
	}
	if item.Status != "active" {
		w.abort(ctx, "服务已不可续期")
		return
	}
	if w.d.ExpectedCycle != nil && p.Cycle != *w.d.ExpectedCycle {
		w.abort(ctx, "有效期已被其他操作更新")
		return
	}
	head := fmt.Sprintf("续期「%s」/「%s」\n当前截止：%s\n约定（仅参考，不会自动填入本次金额）：%s",
		item.Name, periodName(p), periodExpiry(p), termsText(p.Terms))
	switch w.d.Step {
	case "renew_precision":
		w.reply(ctx, head+"\n\n新截止使用哪种方式？", pair(
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		), pair(button("取消", cb("dialog", "cancel"))))
	case "renew_expiration":
		w.reply(ctx, "输入新截止（必须晚于当前截止和今天；"+w.dr.Values["precision"]+"，时区 "+p.Timezone+"）。\n"+expirationHelp, navRows(true, false)...)
	case "renew_amount":
		rows := [][]models.InlineKeyboardButton{}
		if p.Terms.Amount != nil && p.Terms.Currency != nil {
			rows = append(rows, pair(button("使用约定金额", cb("dialog", "use_convention"))))
		}
		rows = append(rows, navRows(true, true)...)
		w.reply(ctx, "本次实际续期金额（可选）：发送“金额 币种”，例如 1000 TWD。\n金额为最小货币单位整数；不会自动采用约定金额。", rows...)
	case "renew_notes":
		w.reply(ctx, "续期备注（普通说明，不要填写密码或 Token）。可跳过。", navRows(true, true)...)
	case "renew_preview":
		w.reply(ctx, renewPreviewText(p, item, w.dr), pair(
			button("确认续期", cb("dialog", "confirm")),
			button("返回修改", cb("dialog", "back")),
		), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "续期流程状态无效")
	}
}

func renewPreviewText(p service.Period, item service.ServiceItem, d draft) string {
	expires := d.Values["expires_at"]
	precision := precisionLabel(d.Values["precision"])
	money := "未填写"
	if amount := strings.TrimSpace(d.Values["amount"]); amount != "" {
		money = amount + " " + strings.ToUpper(strings.TrimSpace(d.Values["currency"])) + "（最小货币单位）"
	}
	notes := strings.TrimSpace(d.Values["notes"])
	if notes == "" {
		notes = "（无）"
	}
	return fmt.Sprintf("请确认续期：\n服务：%s\n有效期：%s\n当前截止：%s\n新截止：%s（%s）\n实际续期金额：%s\n备注：%s\n\n提交时会按当前版本重新校验；若已被其他操作推进，会拒绝写入并保留本对话。",
		item.Name, periodName(p), periodExpiry(p), expires, precision, money, notes)
}

func (w *wizard) textRenew(ctx context.Context, text string) {
	switch w.d.Step {
	case "renew_expiration":
		value, precision := trimmed(text), w.dr.Values["precision"]
		if precision == "" {
			w.stepError(ctx, "请先选择截止方式。", false, false)
			return
		}
		if err := parsePrecisionValue(precision, value); err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("expires_at", value)
		next := nextStep(service.DialogRenew, w.dr, w.d.Step)
		if !w.persist(ctx, next) {
			return
		}
		w.render(ctx)
	case "renew_amount":
		amount, currency, err := parseSpaceMoney(text)
		if err != nil {
			w.stepError(ctx, err.Error(), true, true)
			return
		}
		if amount != nil {
			w.dr.set("amount", strconv.FormatInt(*amount, 10))
			w.dr.set("currency", *currency)
		} else {
			w.dr.del("amount")
			w.dr.del("currency")
		}
		if !w.persist(ctx, "renew_notes") {
			return
		}
		w.render(ctx)
	case "renew_notes":
		w.dr.set("notes", text)
		if !w.persist(ctx, "renew_preview") {
			return
		}
		w.render(ctx)
	default:
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
	}
}

// useConvention fills the amount and currency from the recorded terms only
// after an explicit button click; the user still passes through preview and
// confirmation.
func (w *wizard) useConvention(ctx context.Context) {
	if w.d.Action != service.DialogRenew || w.d.Step != "renew_amount" || w.d.PeriodID == nil {
		w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
		return
	}
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
	if err != nil || p.Terms.Amount == nil || p.Terms.Currency == nil {
		w.failure(ctx, "没有可用的约定金额。", true, true)
		return
	}
	w.dr.set("amount", strconv.FormatInt(*p.Terms.Amount, 10))
	w.dr.set("currency", *p.Terms.Currency)
	if !w.persist(ctx, "renew_notes") {
		return
	}
	w.render(ctx)
}

// confirmRenew submits through RenewFromDialog, whose transaction re-checks
// ownership, status, and the captured cycle.
func (w *wizard) confirmRenew(ctx context.Context) {
	if w.dr.Values["expires_at"] == "" {
		w.stepError(ctx, "续期信息未完成，请返回修改。", false, false)
		return
	}
	in := service.RenewInput{DatePrecision: w.dr.Values["precision"], ExpiresAt: w.dr.Values["expires_at"]}
	if amount, ok := w.dr.get("amount"); ok {
		var value int64
		if _, err := fmt.Sscan(amount, &value); err != nil {
			w.stepError(ctx, "金额已损坏，请返回修改。", false, false)
			return
		}
		currency := w.dr.Values["currency"]
		in.Amount, in.Currency = &value, &currency
	}
	if notes, ok := w.dr.get("notes"); ok && strings.TrimSpace(notes) != "" {
		value := notes
		in.Notes = &value
	}
	renewal, err := w.h.svc.RenewFromDialog(ctx, w.user.ID, in)
	if err != nil {
		hint := "续期失败：" + serviceMessage(err) + "\n草稿仍保留，可返回修改或取消。"
		if service.Code(err) == "conflict" {
			hint = "续期失败：有效期已被其他操作更新，本次未写入。\n草稿仍保留；请取消后重新打开有效期详情查看最新状态。"
		}
		w.h.send(ctx, w.b, w.chatID, hint)
		return
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, renewalText(ctx, w.h.svc, w.user, renewal, "续期完成"), markup(
		pair(button("查看续期历史", cb("period", "renewals", id(renewal.PeriodID), "0")), button("再次续期", cb("period", "renew", id(renewal.PeriodID)))),
		pair(button("返回服务", cb("service", "view", id(renewal.PeriodID), "0"))),
	))
}
