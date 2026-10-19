package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
)

// startRenewWizard is the single entry for /renew <id>, reminder buttons, and
// the period detail "续期" button. An existing different wizard is never
// discarded silently.
func (h *Handler) startRenewWizard(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRenew, periodID, "renew:"+id(periodID)) {
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
	if w.d.ValidityPeriodID != nil {
		periodID = int(*w.d.ValidityPeriodID)
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
	if p.Status == "cancelled" || item.Status != "active" {
		w.abort(ctx, "有效期或服务已不可续期")
		return
	}
	if w.d.ExpectedCycle != nil && p.Cycle != *w.d.ExpectedCycle {
		w.abort(ctx, "周期已被其他操作更新")
		return
	}
	head := fmt.Sprintf("续期：%s / %s\n当前截止：%s\n当前周期：%d\n条款（仅参考，不会自动填入本次金额）：%s",
		item.Name, periodName(p), periodExpiry(p), p.Cycle, termsText(p.Terms))
	if item.SIMDetails == nil {
		head += "\n提醒：本服务没有 SIM 专属资料。"
	}
	switch w.d.Step {
	case "renew_choose_precision":
		w.reply(ctx, head+"\n\n新截止使用哪种精度？", pair(
			button("仅日期 date", choiceCallback("precision", "date")),
			button("日期时间 datetime", choiceCallback("precision", "datetime")),
		), pair(button("取消", cb("dialog", "cancel"))))
	case "renew_expiration":
		w.reply(ctx, head+"\n\n请输入新的截止（"+w.dr.Values["precision"]+"）。\n"+expirationHelp, navRows(true, false)...)
	case "renew_amount":
		w.reply(ctx, "本次实际续期金额（最小货币单位整数），可提交多次输入“金额 币种”一起填写；不需要金额请点“跳过”。\n示例：1000 TWD\n条款金额不会被自动采用。", navRows(true, true)...)
	case "renew_notes":
		w.reply(ctx, "续期备注（普通说明，不要填写密码或 Token）。可跳过。", navRows(true, true)...)
	case "renew_preview":
		w.renderRenewPreview(ctx, p, item)
	default:
		w.abort(ctx, "续期流程状态无效")
	}
}

func (w *wizard) renderRenewPreview(ctx context.Context, p service.Period, item service.ServiceItem) {
	w.reply(ctx, renewPreviewText(p, item, w.dr), pair(
		button("确认续期", cb("dialog", "confirm")),
		button("返回修改", cb("dialog", "back")),
	), pair(button("取消", cb("dialog", "cancel"))))
}

func renewPreviewText(p service.Period, item service.ServiceItem, d draft) string {
	expires := d.Values["expires_at"]
	precision := d.Values["precision"]
	money := "不记录金额"
	if amount := strings.TrimSpace(d.Values["amount"]); amount != "" {
		money = amount + " " + strings.ToUpper(strings.TrimSpace(d.Values["currency"])) + "（最小货币单位）"
	}
	notes := strings.TrimSpace(d.Values["notes"])
	if notes == "" {
		notes = "（无）"
	}
	return fmt.Sprintf("请确认续期：\n服务：%s\n有效期：%s\n当前截止：%s\n当前周期：%d → %d\n新截止：%s（%s）\n实际续期金额：%s\n备注：%s\n\n提交时会按当前周期重新校验；若周期已被其他操作推进，会拒绝写入并保留本对话。",
		item.Name, periodName(p), periodExpiry(p), p.Cycle, p.Cycle+1, expires, precision, money, notes)
}

func (w *wizard) textRenew(ctx context.Context, text string) {
	switch w.d.Step {
	case "renew_expiration":
		value, precision := trimmed(text), w.dr.Values["precision"]
		if precision == "" {
			w.stepError(ctx, "请先选择精度。", false, false)
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

// parseSpaceMoney accepts "1000 TWD" or an empty line. A currency is never
// inferred from the user's locale.
func parseSpaceMoney(text string) (*int64, *string, error) {
	fields := strings.Fields(text)
	switch len(fields) {
	case 0:
		return nil, nil, nil
	case 2:
		return parseMoney(fields[0], fields[1])
	default:
		return nil, nil, fail("请发送“金额 币种”，例如 1000 TWD。")
	}
}

// confirmRenew re-reads the period and submits through RenewFromDialog, whose
// transaction re-checks ownership, status, and the captured cycle.
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
			hint = "续期失败：周期已被其他操作更新，本次未写入。\n草稿仍保留；请取消后重新打开有效期详情查看最新状态。"
		}
		w.h.send(ctx, w.b, w.chatID, hint)
		return
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, renewalText(ctx, w.h.svc, w.user, renewal, "续期完成"), markup(
		pair(button("查看有效期", cb("period", "view", id(renewal.ValidityPeriodID))), button("查看续期历史", cb("period", "renewals", id(renewal.ValidityPeriodID)))),
	))
}
