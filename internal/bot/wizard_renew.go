package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"
)

// startRenewWizard opens the renewal flow for one period. An existing
// different wizard is never discarded silently.
func (h *Handler) startRenewWizard(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID int64) {
	if h.conflictTarget(ctx, b, user, resp, service.DialogRenew, periodID, service.DialogRenew+":"+id(periodID)) {
		return
	}
	d, err := h.svc.StartPeriodRenewDialog(ctx, user.ID, periodID, resp.chatID)
	if err != nil {
		h.send(ctx, b, resp, "无法开始续期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, d, resp)
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
	head := fmt.Sprintf("续期「%s」/「%s」\n当前截止：%s\n约定（仅供参考）：%s",
		item.Name, periodName(p), periodExpiry(p), termsText(p.Terms))
	switch w.d.Step {
	case "renew_precision":
		w.reply(ctx, head+"\n\n新截止使用哪种方式？", []models.InlineKeyboardButton{
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	case "renew_expiration":
		w.reply(ctx, "输入新截止（必须晚于当前截止和今天；"+precisionLabel(w.dr.Values["precision"])+"，时区 "+p.Timezone+"）。\n"+expirationHint(w.dr.Values["precision"]), navRows(true, false)...)
	case "renew_amount":
		rows := [][]models.InlineKeyboardButton{}
		if p.Terms.Amount != nil && p.Terms.Currency != nil {
			rows = append(rows, []models.InlineKeyboardButton{button("使用约定金额", cb("dialog", "use_convention"))})
		}
		rows = append(rows, navRows(true, true)...)
		w.reply(ctx, "本次实际续期金额（可选）：发送“金额 币种”，例如 10.30 CNY。", rows...)
	case "renew_notes":
		w.reply(ctx, "续期备注（可选）。", navRows(true, true)...)
	case "renew_preview":
		w.reply(ctx, renewPreviewText(p, item, w.dr), []models.InlineKeyboardButton{
			button("确认续期", cb("dialog", "confirm")),
			button("返回修改", cb("dialog", "back")),
		}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func renewPreviewText(p service.Period, item service.ServiceItem, d draft) string {
	expires := wallClockDisplay(d.Values["expires_at"], p.Timezone)
	precision := precisionLabel(d.Values["precision"])
	money := "未填写"
	if amount := strings.TrimSpace(d.Values["amount"]); amount != "" {
		currency := strings.ToUpper(strings.TrimSpace(d.Values["currency"]))
		if value, err := strconv.ParseInt(amount, 10, 64); err == nil {
			money = domain.FormatMoney(value, currency) + " " + currency
		}
	}
	notes := strings.TrimSpace(d.Values["notes"])
	if notes == "" {
		notes = "（无）"
	}
	return fmt.Sprintf("请确认续期：\n服务：%s\n有效期：%s\n当前截止：%s\n新截止：%s（%s）\n实际续期金额：%s\n备注：%s",
		item.Name, periodName(p), periodExpiry(p), expires, precision, money, notes)
}

func (w *wizard) textRenew(ctx context.Context, text string) {
	switch w.d.Step {
	case "renew_expiration":
		precision := w.dr.Values["precision"]
		if precision == "" {
			w.stepError(ctx, "请先选择截止方式。", false, false)
			return
		}
		if w.d.PeriodID == nil {
			w.abort(ctx, "该流程已失效")
			return
		}
		p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
		if err != nil {
			w.abort(ctx, "有效期已不存在")
			return
		}
		normalized, err := normalizePrecisionValue(precision, trimmed(text), p.Timezone)
		if err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("expires_at", normalized)
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
		w.stepError(ctx, "请用下方按钮选择。", false, false)
	}
}

// useConvention fills the amount and currency from the recorded terms only
// after an explicit button click; the user still passes through preview and
// confirmation.
func (w *wizard) useConvention(ctx context.Context) {
	if w.d.Action != service.DialogRenew || w.d.Step != "renew_amount" || w.d.PeriodID == nil {
		w.failure(ctx, expiredButtonMessage, false, false)
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
		w.stepError(ctx, expiredButtonMessage, false, false)
		return
	}
	in := service.RenewInput{DatePrecision: w.dr.Values["precision"], ExpiresAt: w.dr.Values["expires_at"]}
	if amount, ok := w.dr.get("amount"); ok {
		var value int64
		if _, err := fmt.Sscan(amount, &value); err != nil {
			w.stepError(ctx, expiredButtonMessage, false, false)
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
		hint := "续期失败：" + serviceMessage(err) + "\n草稿已保留。"
		if service.Code(err) == "conflict" {
			hint = "续期失败：数据已更新，本次未写入。\n草稿已保留。"
		}
		w.h.send(ctx, w.b, w.resp, hint)
		return
	}
	w.h.sendMarkup(ctx, w.b, w.resp, renewalText(ctx, w.h.svc, w.user, renewal, "续期完成"), markup(
		[]models.InlineKeyboardButton{button("查看续期历史", cb("period", "renewals", id(renewal.PeriodID), "0")), button("再次续期", cb("period", "renew", id(renewal.PeriodID)))},
		[]models.InlineKeyboardButton{button("返回服务", cb("service", "view", id(renewal.PeriodID), "0"))},
	))
}
