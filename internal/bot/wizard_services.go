package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ---- service add -----------------------------------------------------------

func (h *Handler) startServiceAdd(ctx context.Context, b *telegram.Bot, user service.User, resp *responder) {
	if h.conflictTarget(ctx, b, user, resp, service.DialogServiceAdd, 0, service.DialogServiceAdd) {
		return
	}
	d := service.Dialog{Step: "add_category", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogServiceAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始添加服务："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.renderServiceAdd(ctx)
}

func (w *wizard) renderServiceAdd(ctx context.Context) {
	switch w.d.Step {
	case "add_category":
		rows, err := categoryRows(ctx, w.h, w.user.ID)
		if err != nil {
			w.abort(ctx, "读取类别失败")
			return
		}
		w.reply(ctx, "添加服务 · 第 1 步，选择类别。", rows...)
	case "add_category_name":
		w.reply(ctx, "新类别名称（最多 200 字）。", navRows(true, false)...)
	case "add_name":
		w.reply(ctx, "第 2 步，输入服务名称（例如：东京 VPS）。", navRows(true, false)...)
	case "add_card_type":
		w.reply(ctx, "选择卡类型。", []models.InlineKeyboardButton{
			button("实体卡", choiceCallback("card_type", "physical_sim")),
			button("eSIM", choiceCallback("card_type", "esim")),
		}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
	case "add_provider":
		w.reply(ctx, "可选：服务商/运营商。", optionalNav(true)...)
	case "add_identifier":
		w.reply(ctx, "可选：公开标识（主机名、域名或账号标识）。", optionalNav(true)...)
	case "add_management_url":
		w.reply(ctx, "可选：管理链接（http(s) 绝对地址，不能带账号密码）。", optionalNav(true)...)
	case "add_phone":
		w.reply(ctx, "可选：号码。", optionalNav(true)...)
	case "add_region":
		w.reply(ctx, "可选：国家或地区。", optionalNav(true)...)
	case "add_iccid":
		w.reply(ctx, "可选：ICCID。", optionalNav(true)...)
	case "add_notes":
		w.reply(ctx, "可选：备注（最多 4000 字）。", optionalNav(true)...)
	case "add_preview":
		w.renderServiceAddPreview(ctx)
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func (w *wizard) renderServiceAddPreview(ctx context.Context) {
	categoryName, kind := "未知类别", w.dr.Values["kind"]
	for _, c := range mustCategories(ctx, w.h, w.user.ID) {
		if id(c.ID) == w.dr.Values["category_id"] {
			categoryName, kind = c.Name, c.Kind
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "请确认新建服务：\n类别：%s\n名称：%s\n服务商：%s\n", categoryName, w.dr.Values["name"], shown(w.dr, "provider"))
	if kind != "sim" {
		fmt.Fprintf(&b, "公开标识：%s\n", shown(w.dr, "identifier"))
	}
	fmt.Fprintf(&b, "管理链接：%s\n备注：%s\n", shown(w.dr, "management_url"), shown(w.dr, "notes"))
	if kind == "sim" {
		fmt.Fprintf(&b, "卡类型：%s\n号码：%s\n国家/地区：%s\nICCID：%s\n", cardTypeLabel(w.dr.Values["card_type"]), shown(w.dr, "phone"), shown(w.dr, "region"), shown(w.dr, "iccid"))
	}

	w.reply(ctx, b.String(), []models.InlineKeyboardButton{button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
}

func cardTypeLabel(value string) string {
	switch value {
	case "physical_sim":
		return "实体 SIM"
	case "esim":
		return "eSIM"
	}
	return value
}

func shown(d draft, key string) string {
	v, ok := d.get(key)
	if !ok || strings.TrimSpace(v) == "" {
		if ok {
			return "（已清空）"
		}
		return "（未填写）"
	}
	return v
}

func (w *wizard) textServiceAdd(ctx context.Context, text string) {
	value := trimmed(text)
	switch w.d.Step {
	case "add_category_name":
		if value == "" || len([]rune(value)) > 200 {
			w.stepError(ctx, "类别名称必填且不超过 200 字。", true, false)
			return
		}
		if _, err := w.h.svc.CreateCategory(ctx, w.user.ID, value); err != nil {
			if service.Code(err) == "conflict" {
				w.stepError(ctx, "已有同名自定义类别，请换一个名称。", true, false)
				return
			}
			w.stepError(ctx, "创建类别失败："+serviceMessage(err), true, false)
			return
		}
		if !w.persist(ctx, "add_category") {
			return
		}
		w.render(ctx)
		return
	case "add_name":
		if value == "" || len([]rune(value)) > 200 {
			w.stepError(ctx, "名称必填且不超过 200 字。", true, false)
			return
		}
		w.dr.set("name", value)
	case "add_provider", "add_identifier", "add_notes", "add_phone", "add_region", "add_iccid":
		key := map[string]string{"add_provider": "provider", "add_identifier": "identifier", "add_notes": "notes", "add_phone": "phone", "add_region": "region", "add_iccid": "iccid"}[w.d.Step]
		w.dr.set(key, value)
	case "add_management_url":
		if err := validateManagementURL(value); err != nil {
			w.stepError(ctx, err.Error(), true, true)
			return
		}
		w.dr.set("management_url", value)
	default:
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	next := nextStep(service.DialogServiceAdd, w.dr, w.d.Step)
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func (w *wizard) confirmServiceAdd(ctx context.Context) {
	categoryID, err := strconv.ParseInt(w.dr.Values["category_id"], 10, 64)
	if err != nil || categoryID < 1 || w.dr.Values["name"] == "" {
		w.stepError(ctx, expiredButtonMessage, false, false)
		return
	}
	in := service.ServiceInput{Name: w.dr.Values["name"], CategoryID: categoryID,
		Provider: opt(w.dr, "provider"), ManagementURL: opt(w.dr, "management_url"), Notes: opt(w.dr, "notes")}
	if w.dr.Values["kind"] == "sim" {
		in.SIMDetails = &service.SIMDetails{CardType: w.dr.Values["card_type"], PhoneNumber: opt(w.dr, "phone"),
			CountryOrRegion: opt(w.dr, "region"), ICCID: opt(w.dr, "iccid")}
		w.dr.del("identifier")
	} else {
		in.Identifier = opt(w.dr, "identifier")
		for _, key := range []string{"card_type", "phone", "region", "iccid"} {
			w.dr.del(key)
		}
	}
	item, err := w.h.svc.CreateService(ctx, w.user.ID, in)
	if err != nil {
		w.h.send(ctx, w.b, w.resp, "创建失败："+serviceMessage(err)+"\n草稿已保留。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.showServiceDetail(ctx, w.b, w.user, w.resp, item.ID, 0)
}

func opt(d draft, key string) *string {
	v, ok := d.get(key)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func nullable(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func categoryRows(ctx context.Context, h *Handler, uid int64) ([][]models.InlineKeyboardButton, error) {
	categories, err := mustCategoriesOrErr(ctx, h, uid)
	if err != nil {
		return nil, err
	}
	rows := [][]models.InlineKeyboardButton{}
	row := []models.InlineKeyboardButton{}
	for _, c := range categories {
		label := c.Name
		if c.Kind == "sim" {
			label += "（SIM）"
		}
		row = append(row, button(label, choiceCallback("category", id(c.ID))))
		if len(row) == 2 {
			rows = append(rows, row)
			row = []models.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows,
		[]models.InlineKeyboardButton{button("新建类别…", cb("dialog", "new_category"))},
		[]models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))},
	)
	return rows, nil
}

func mustCategories(ctx context.Context, h *Handler, uid int64) []service.Category {
	out, _ := mustCategoriesOrErr(ctx, h, uid)
	return out
}

func mustCategoriesOrErr(ctx context.Context, h *Handler, uid int64) ([]service.Category, error) {
	var out []service.Category
	for after := int64(0); ; {
		page, err := h.svc.ListCategories(ctx, uid, after, 100)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Data...)
		if page.NextCursor == nil {
			return out, nil
		}
		after = *page.NextCursor
	}
}

// ---- service edit ---------------------------------------------------------

func (h *Handler) startServiceEdit(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, serviceID int64) {
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		h.send(ctx, b, resp, "无法编辑："+serviceMessage(err))
		return
	}
	if h.conflictTarget(ctx, b, user, resp, service.DialogServiceEdit, serviceID, service.DialogServiceEdit+":"+id(serviceID)) {
		return
	}
	d := service.Dialog{ServiceID: &serviceID, Step: "edit_field", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogServiceEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.dr.set("kind", kindOf(item))
	if !w.persist(ctx, "edit_field") {
		return
	}
	w.renderServiceEdit(ctx)
}

func kindOf(item service.ServiceItem) string {
	if item.SIMDetails != nil {
		return "sim"
	}
	return "generic"
}

func (w *wizard) renderServiceEdit(ctx context.Context) {
	item, err := w.h.svc.GetService(ctx, w.user.ID, *w.d.ServiceID)
	if err != nil {
		w.abort(ctx, "服务已不存在")
		return
	}
	kind := kindOf(item)
	w.dr.set("kind", kind)
	category := categoryName(ctx, w.h, w.user.ID, item.CategoryID)
	switch w.d.Step {
	case "edit_field":
		w.reply(ctx, "编辑服务「"+item.Name+"」\n选择要修改的字段。", editFieldRows(item)...)
	case "edit_value":
		field := w.dr.Values["field"]
		if field == "category_id" {
			rows := [][]models.InlineKeyboardButton{}
			row := []models.InlineKeyboardButton{}
			for _, c := range mustCategories(ctx, w.h, w.user.ID) {
				if c.Kind != kind {
					continue
				}
				row = append(row, button(c.Name, choiceCallback("category", id(c.ID))))
				if len(row) == 2 {
					rows = append(rows, row)
					row = []models.InlineKeyboardButton{}
				}
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
			rows = append(rows, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
			w.reply(ctx, "当前类别："+category+"\n\n选择新的类别。", rows...)
			return
		}
		if field == "card_type" {
			w.reply(ctx, "当前卡类型："+editValue(item, field, category), []models.InlineKeyboardButton{
				button("实体卡", choiceCallback("card_type", "physical_sim")),
				button("eSIM", choiceCallback("card_type", "esim")),
			}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
			return
		}
		rows := navRows(true, false)
		if field != "name" {
			rows = append([][]models.InlineKeyboardButton{[]models.InlineKeyboardButton{button("清空当前值", cb("dialog", "clear_value"))}}, rows...)
		}
		w.reply(ctx, fmt.Sprintf("当前值：%s\n\n新的%s：", editValue(item, field, category), editLabel(field)), rows...)
	case "edit_preview":
		field := w.dr.Values["field"]
		newValue := draftEditValue(w.dr, field)
		if field == "category_id" {
			if n, err := strconv.ParseInt(w.dr.Values["category_id"], 10, 64); err == nil {
				newValue = categoryName(ctx, w.h, w.user.ID, n)
			}
		}
		if field == "card_type" {
			newValue = cardTypeLabel(w.dr.Values["card_type"])
		}
		w.reply(ctx, fmt.Sprintf("请确认修改服务「%s」：\n字段：%s\n旧值：%s\n新值：%s",
			item.Name, editLabel(field), editValue(item, field, category), newValue),
			[]models.InlineKeyboardButton{button("确认修改", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))},
			[]models.InlineKeyboardButton{button("改其他字段", cb("dialog", "change_field")), button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func editLabel(field string) string {
	for _, candidate := range serviceEditFields {
		if candidate.key == field {
			return candidate.label
		}
	}
	return field
}

func editValue(item service.ServiceItem, field string, category string) string {
	switch field {
	case "name":
		return item.Name
	case "provider":
		return textOrDash(item.Provider)
	case "identifier":
		return textOrDash(item.Identifier)
	case "management_url":
		return textOrDash(item.ManagementURL)
	case "notes":
		return textOrDash(item.Notes)
	case "category_id":
		return category
	}
	if d := item.SIMDetails; d != nil {
		switch field {
		case "card_type":
			return cardTypeLabel(d.CardType)
		case "phone":
			return textOrDash(d.PhoneNumber)
		case "region":
			return textOrDash(d.CountryOrRegion)
		case "iccid":
			return textOrDash(d.ICCID)
		}
	}
	return "未填写"
}

func editFieldRows(item service.ServiceItem) [][]models.InlineKeyboardButton {
	rows := [][]models.InlineKeyboardButton{}
	row := []models.InlineKeyboardButton{}
	for _, field := range serviceEditFields {
		// A SIM service keeps its public facts in the SIM detail block, and a
		// generic service has no SIM detail block at all.
		if field.simOnly && item.SIMDetails == nil {
			continue
		}
		if field.genericOnly && item.SIMDetails != nil {
			continue
		}
		text := field.button
		if text == "" {
			text = field.label
		}
		row = append(row, button(text, choiceCallback("field", field.key)))
		if len(row) == 2 {
			rows = append(rows, row)
			row = []models.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{button("返回服务", cb("service", "view", id(item.ID), "0")), button("取消", cb("dialog", "cancel"))})
	return rows
}

func (w *wizard) textServiceEdit(ctx context.Context, text string) {
	if w.d.Step != "edit_value" {
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	field, value := w.dr.Values["field"], trimmed(text)
	if field == "category_id" || field == "card_type" {
		w.stepError(ctx, "请用下方按钮选择。", true, false)
		return
	}
	switch field {
	case "name":
		if value == "" || len([]rune(value)) > 200 {
			w.stepError(ctx, "名称必填且不超过 200 字。", true, false)
			return
		}
	case "management_url":
		if err := validateManagementURL(value); err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
	}
	w.dr.set("value", value)
	if !w.persist(ctx, "edit_preview") {
		return
	}
	w.render(ctx)
}

func draftEditValue(d draft, field string) string {
	if field == "category_id" || field == "card_type" {
		return "（已选择）"
	}
	return shown(d, "value")
}

func (w *wizard) confirmServiceEdit(ctx context.Context) {
	field, value := w.dr.Values["field"], w.dr.Values["value"]
	patch := service.ServicePatch{}
	set := func(dest *service.OptionalString) {
		*dest = service.OptionalString{Set: true, Value: nullable(value)}
	}
	switch field {
	case "name":
		patch.Name = &value
	case "category_id":
		n, err := strconv.ParseInt(w.dr.Values["category_id"], 10, 64)
		if err != nil || n < 1 {
			w.stepError(ctx, expiredButtonMessage, false, false)
			return
		}
		patch.CategoryID = &n
	case "provider":
		set(&patch.Provider)
	case "identifier":
		item, err := w.h.svc.GetService(ctx, w.user.ID, *w.d.ServiceID)
		if err != nil {
			w.stepError(ctx, "服务已不存在，请重新打开。", false, false)
			return
		}
		if item.SIMDetails != nil {
			w.stepError(ctx, "SIM 服务不使用公开标识，请返回选择其他字段。", false, false)
			return
		}
		set(&patch.Identifier)
	case "management_url":
		set(&patch.ManagementURL)
	case "notes":
		set(&patch.Notes)
	case "card_type":
		patch.SIMDetailsSet = true
		patch.SIMDetails = &service.SIMDetailsPatch{CardType: service.OptionalString{Set: true, Value: &value}}
	case "phone", "region", "iccid":
		details := service.SIMDetailsPatch{}
		switch field {
		case "phone":
			details.PhoneNumber = service.OptionalString{Set: true, Value: nullable(value)}
		case "region":
			details.CountryOrRegion = service.OptionalString{Set: true, Value: nullable(value)}
		case "iccid":
			details.ICCID = service.OptionalString{Set: true, Value: nullable(value)}
		}
		patch.SIMDetailsSet = true
		patch.SIMDetails = &details
	default:
		w.stepError(ctx, expiredButtonMessage, false, false)
		return
	}
	item, err := w.h.svc.UpdateService(ctx, w.user.ID, *w.d.ServiceID, patch)
	if err != nil {
		w.h.send(ctx, w.b, w.resp, "修改失败："+serviceMessage(err)+"\n草稿已保留。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.showServiceDetail(ctx, w.b, w.user, w.resp, item.ID, 0)
}

// ---- period add -----------------------------------------------------------

func (h *Handler) startPeriodAdd(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, serviceID int64) {
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		h.send(ctx, b, resp, "无法添加有效期："+serviceMessage(err))
		return
	}
	if item.Status != "active" {
		h.send(ctx, b, resp, "服务已归档，请先恢复服务再添加有效期。")
		return
	}
	if h.conflictTarget(ctx, b, user, resp, service.DialogPeriodAdd, serviceID, service.DialogPeriodAdd+":"+id(serviceID)) {
		return
	}
	d := service.Dialog{ServiceID: &serviceID, Step: "period_name", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogPeriodAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始添加有效期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.renderPeriodAdd(ctx)
}

func (w *wizard) renderPeriodAdd(ctx context.Context) {
	item, err := w.h.svc.GetService(ctx, w.user.ID, *w.d.ServiceID)
	if err != nil {
		w.abort(ctx, "服务已不存在")
		return
	}
	switch w.d.Step {
	case "period_name":
		w.reply(ctx, fmt.Sprintf("添加有效期到「%s」· 第 1 步，有效期名称（可选）。", item.Name), optionalNav(true)...)
	case "period_precision":
		w.reply(ctx, "第 2 步，截止方式。\n仅日期表示当日结束前有效；精确时间保持绝对时刻。", []models.InlineKeyboardButton{
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
	case "period_expires":
		w.reply(ctx, "第 3 步，输入截止（"+precisionLabel(w.dr.Values["precision"])+"，时区 "+w.user.Timezone+"）。\n"+expirationHint(w.dr.Values["precision"]), navRows(true, false)...)
	case "period_starts":
		w.reply(ctx, "第 4 步，开始时间（可选，按你的时区，必须早于截止）。\n示例：2027-01-01 00:00", optionalNav(true)...)
	case "terms_mode":
		w.reply(ctx, "第 5 步，续费约定（可选，只作记录，不影响提醒日期）。", []models.InlineKeyboardButton{
			button("一次性", choiceCallback("mode", "once")),
			button("周期性", choiceCallback("mode", "recurring")),
			button("暂不设置", choiceCallback("mode", "custom")),
		}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
	case "terms_interval":
		w.reply(ctx, "周期长度，例如：1 month、2 week。单位可为 day/week/month/year。", navRows(true, false)...)
	case "terms_amount":
		w.reply(ctx, "约定金额（可选）：发送“金额 币种”，例如 10.30 CNY。", optionalNav(true)...)
	case "period_preview":
		w.reply(ctx, periodAddPreviewText(item, w.dr, w.user.Timezone), []models.InlineKeyboardButton{button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func termsDraft(d draft) service.PeriodTerms {
	t := service.PeriodTerms{Mode: d.Values["mode"], Revision: 1}
	if t.Mode == "" {
		t.Mode = "custom"
	}
	if t.Mode == "recurring" {
		if n, err := strconv.ParseInt(d.Values["interval_count"], 10, 64); err == nil {
			t.IntervalCount = &n
		}
		if unit := d.Values["interval_unit"]; unit != "" {
			t.IntervalUnit = &unit
		}
	}
	if amount := strings.TrimSpace(d.Values["amount"]); amount != "" {
		if n, err := strconv.ParseInt(amount, 10, 64); err == nil {
			t.Amount = &n
		}
		currency := strings.ToUpper(strings.TrimSpace(d.Values["currency"]))
		t.Currency = &currency
	}
	return t
}

func termsFromDraft(d draft) *service.TermsInput {
	t := termsDraft(d)
	if t.Mode == "custom" && t.Amount == nil {
		return nil
	}
	return &service.TermsInput{Mode: t.Mode, IntervalCount: t.IntervalCount, IntervalUnit: t.IntervalUnit, Amount: t.Amount, Currency: t.Currency}
}

func periodAddPreviewText(item service.ServiceItem, d draft, zone string) string {
	expires := wallClockDisplay(d.Values["expires_at"], zone)
	starts := shown(d, "starts_at")
	if value := d.Values["starts_at"]; strings.TrimSpace(value) != "" {
		starts = wallClockDisplay(value, zone)
	}
	return fmt.Sprintf("请确认新建有效期：\n服务：%s\n名称：%s\n截止方式：%s\n截止：%s\n开始：%s\n约定：%s",
		item.Name, shown(d, "name"), precisionLabel(d.Values["precision"]), expires, starts, termsText(termsDraft(d)))
}

func precisionLabel(value string) string {
	switch value {
	case "date":
		return "仅日期"
	case "datetime":
		return "精确时间"
	}
	return value
}

func (w *wizard) textPeriodAdd(ctx context.Context, text string) {
	value := trimmed(text)
	switch w.d.Step {
	case "period_name":
		if len([]rune(value)) > 200 {
			w.stepError(ctx, "名称最多 200 字。", true, true)
			return
		}
		w.dr.set("name", value)
	case "period_expires":
		normalized, err := normalizePrecisionValue(w.dr.Values["precision"], value, w.user.Timezone)
		if err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("expires_at", normalized)
	case "period_starts":
		if value != "" {
			normalized, err := normalizeDateTime(value, w.user.Timezone)
			if err != nil {
				w.stepError(ctx, err.Error(), true, true)
				return
			}
			at, _ := time.Parse(time.RFC3339, normalized)
			expires, err := parseExpiryForStarts(w.dr.Values["precision"], w.dr.Values["expires_at"], w.user.Timezone)
			if err != nil || !at.Before(expires) {
				w.stepError(ctx, "开始时间必须早于截止时间。", true, true)
				return
			}
			value = normalized
		}
		w.dr.set("starts_at", value)
	case "terms_interval":
		count, unit, err := parseInterval(value)
		if err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("interval_count", id(count))
		w.dr.set("interval_unit", unit)
	case "terms_amount":
		if value == "" {
			w.dr.del("amount")
			w.dr.del("currency")
			break
		}
		amount, currency, err := parseSpaceMoney(value)
		if err != nil {
			w.stepError(ctx, err.Error(), true, true)
			return
		}
		if amount == nil {
			w.stepError(ctx, "请发送“金额 币种”，例如 2999 USD。", true, true)
			return
		}
		w.dr.set("amount", strconv.FormatInt(*amount, 10))
		w.dr.set("currency", *currency)
	default:
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	next := nextStep(service.DialogPeriodAdd, w.dr, w.d.Step)
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func parseExpiryForStarts(precision, value, zone string) (time.Time, error) {
	if precision == "date" {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, err
		}
		return time.ParseInLocation("2006-01-02", value, loc)
	}
	return time.Parse(time.RFC3339, value)
}

func parseInterval(value string) (int64, string, error) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return 0, "", fail("请发送“数量 单位”，例如 1 month。")
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || n < 1 {
		return 0, "", fail("数量必须是正整数。")
	}
	switch fields[1] {
	case "day", "week", "month", "year":
	default:
		return 0, "", fail("单位必须是 day、week、month 或 year。")
	}
	return n, fields[1], nil
}

func (w *wizard) confirmPeriodAdd(ctx context.Context) {
	in := service.PeriodInput{Name: w.dr.Values["name"],
		DatePrecision: w.dr.Values["precision"], ExpiresAt: w.dr.Values["expires_at"]}
	if starts := strings.TrimSpace(w.dr.Values["starts_at"]); starts != "" {
		in.StartsAt = &starts
	}
	in.Terms = termsFromDraft(w.dr)
	p, err := w.h.svc.CreatePeriod(ctx, w.user.ID, *w.d.ServiceID, in)
	if err != nil {
		w.h.send(ctx, w.b, w.resp, "创建有效期失败："+serviceMessage(err)+"\n草稿已保留。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	rules, _ := w.h.svc.ListRules(ctx, w.user.ID, p.ID, 0, 1)
	w.h.sendMarkup(ctx, w.b, w.resp, periodDetail(p, len(rules.Data)), markup(
		[]models.InlineKeyboardButton{button("设置提醒", cb("rule", "add", id(p.ID))), button("立即续期", cb("period", "renew", id(p.ID)))},
		[]models.InlineKeyboardButton{button("查看有效期", cb("period", "view", id(p.ID))), button("返回服务", cb("service", "view", id(p.ServiceID), "0"))},
	))
}

// ---- period edit ----------------------------------------------------------

func (h *Handler) startPeriodEdit(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, resp, "无法编辑有效期："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, resp, "服务已归档，请恢复服务后再编辑有效期。")
		return
	}
	if h.conflictTarget(ctx, b, user, resp, service.DialogPeriodEdit, periodID, service.DialogPeriodEdit+":"+id(periodID)) {
		return
	}
	cycle, revision := p.Cycle, p.Terms.Revision
	serviceID := p.ServiceID
	d := service.Dialog{ServiceID: &serviceID, PeriodID: &periodID, ExpectedCycle: &cycle, ExpectedTermsRevision: &revision, Step: "edit_field", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogPeriodEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始编辑有效期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.renderPeriodEdit(ctx)
}

func (w *wizard) renderPeriodEdit(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	switch w.d.Step {
	case "edit_field":
		rows := [][]models.InlineKeyboardButton{}
		row := []models.InlineKeyboardButton{}
		for _, field := range periodEditFields {
			row = append(row, button(field.label, choiceCallback("pfield", field.key)))
			if len(row) == 2 {
				rows = append(rows, row)
				row = []models.InlineKeyboardButton{}
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
		rows = append(rows, []models.InlineKeyboardButton{button("返回有效期", cb("period", "view", id(p.ID))), button("取消", cb("dialog", "cancel"))})
		w.reply(ctx, "编辑有效期「"+periodName(p)+"」\n选择要修改的字段。", rows...)
	case "edit_value":
		field := w.dr.Values["field"]
		if field == "name" {
			w.reply(ctx, "当前名称："+shownValue(p.Name)+"\n\n新名称（留空表示不设置）：", navRows(true, false)...)
		} else {
			rows := navRows(true, false)
			if p.StartsAt != nil {
				rows = append([][]models.InlineKeyboardButton{[]models.InlineKeyboardButton{button("清除开始时间", cb("dialog", "clear_value"))}}, rows...)
			}
			w.reply(ctx, "当前开始："+startValue(p.StartsAt, p.Timezone)+"\n\n新开始时间，例如 2027-01-01 00:00（按 "+p.Timezone+"）。", rows...)
		}
	case "period_precision":
		w.reply(ctx, "选择新的截止方式。\n仅日期表示当日结束前有效；精确时间保持绝对时刻。", []models.InlineKeyboardButton{
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
	case "period_expires":
		w.reply(ctx, "当前截止："+periodExpiry(p)+"\n\n新截止（"+precisionLabel(w.dr.Values["precision"])+"，时区 "+p.Timezone+"）。\n"+expirationHint(w.dr.Values["precision"]), navRows(true, false)...)
	case "terms_mode":
		w.reply(ctx, "当前约定："+termsText(p.Terms)+"\n\n选择新的条款模式。", []models.InlineKeyboardButton{
			button("一次性", choiceCallback("mode", "once")),
			button("周期性", choiceCallback("mode", "recurring")),
			button("暂不设置", choiceCallback("mode", "custom")),
		}, []models.InlineKeyboardButton{button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))})
	case "terms_interval":
		w.reply(ctx, "周期长度，例如：1 month、2 week。单位可为 day/week/month/year。", navRows(true, false)...)
	case "terms_amount":
		w.reply(ctx, "约定金额（可选）：发送“金额 币种”，例如 10.30 CNY。", optionalNav(true)...)
	case "edit_preview":
		w.reply(ctx, periodEditPreviewText(p, w.dr), []models.InlineKeyboardButton{button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func shownValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "（未设置）"
	}
	return v
}

func startValue(v *int64, zone string) string {
	if v == nil {
		return "（未设置）"
	}
	return wallClockDisplay(time.Unix(*v, 0).UTC().Format(time.RFC3339), zone)
}

func periodEditPreviewText(p service.Period, d draft) string {
	field := d.Values["field"]
	old, next := "", ""
	switch field {
	case "name":
		old, next = shownValue(p.Name), shownValue(d.Values["value"])
	case "starts":
		old, next = startValue(p.StartsAt, p.Timezone), shownValue(wallClockDisplay(d.Values["value"], p.Timezone))
	case "expires":
		old, next = periodExpiry(p), wallClockDisplay(d.Values["expires_at"], p.Timezone)+"（"+precisionLabel(d.Values["precision"])+"）"
	case "terms":
		old, next = termsText(p.Terms), termsText(termsDraft(d))
	}
	return fmt.Sprintf("请确认修改有效期「%s」：\n字段：%s\n旧值：%s\n新值：%s",
		periodName(p), periodFieldLabel(field), old, next)
}

func periodFieldLabel(field string) string {
	for _, candidate := range periodEditFields {
		if candidate.key == field {
			return candidate.label
		}
	}
	return field
}

func (w *wizard) textPeriodEdit(ctx context.Context, text string) {
	value := trimmed(text)
	loadPeriod := func() (service.Period, bool) {
		if w.d.PeriodID == nil {
			w.abort(ctx, "该流程已失效")
			return service.Period{}, false
		}
		p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
		if err != nil {
			w.abort(ctx, "有效期已不存在")
			return service.Period{}, false
		}
		return p, true
	}
	switch w.d.Step {
	case "edit_value":
		field := w.dr.Values["field"]
		if field == "name" {
			if len([]rune(value)) > 200 {
				w.stepError(ctx, "名称最多 200 字。", true, false)
				return
			}
		} else if field == "starts" && value != "" {
			p, ok := loadPeriod()
			if !ok {
				return
			}
			normalized, err := normalizeDateTime(value, p.Timezone)
			if err != nil {
				w.stepError(ctx, err.Error(), true, false)
				return
			}
			at, _ := time.Parse(time.RFC3339, normalized)
			if !at.Before(time.Unix(p.ExpiresAt, 0)) {
				w.stepError(ctx, "开始时间必须早于截止时间。", true, false)
				return
			}
			value = normalized
		}
		w.dr.set("value", value)
	case "period_expires":
		p, ok := loadPeriod()
		if !ok {
			return
		}
		normalized, err := normalizePrecisionValue(w.dr.Values["precision"], value, p.Timezone)
		if err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("expires_at", normalized)
	case "terms_interval":
		count, unit, err := parseInterval(value)
		if err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("interval_count", id(count))
		w.dr.set("interval_unit", unit)
	case "terms_amount":
		if value == "" || value == "无" {
			w.dr.del("amount")
			w.dr.del("currency")
			break
		}
		amount, currency, err := parseSpaceMoney(value)
		if err != nil {
			w.stepError(ctx, err.Error(), true, true)
			return
		}
		if amount == nil {
			w.stepError(ctx, "请发送“金额 币种”，例如 2999 USD。", true, true)
			return
		}
		w.dr.set("amount", strconv.FormatInt(*amount, 10))
		w.dr.set("currency", *currency)
	default:
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	next := nextStep(service.DialogPeriodEdit, w.dr, w.d.Step)
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func (w *wizard) confirmPeriodEdit(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	if w.d.ExpectedCycle == nil || p.Cycle != *w.d.ExpectedCycle {
		w.h.send(ctx, w.b, w.resp, "有效期已被其他操作更新，本次未写入。\n草稿已保留。")
		return
	}
	field := w.dr.Values["field"]
	patch := service.PeriodPatch{ExpectedCycle: *w.d.ExpectedCycle}
	switch field {
	case "name":
		value := w.dr.Values["value"]
		patch.Name = &value
	case "starts":
		patch.StartsAt = service.OptionalString{Set: true, Value: nullable(w.dr.Values["value"])}
	case "expires":
		precision := w.dr.Values["precision"]
		expires := w.dr.Values["expires_at"]
		patch.DatePrecision, patch.ExpiresAt = &precision, &expires
	case "terms":
		if w.d.ExpectedTermsRevision == nil {
			w.abort(ctx, "该流程已失效")
			return
		}
		patch.ExpectedTermsRevision = w.d.ExpectedTermsRevision
		patch.TermsSet = true
		patch.Terms = termsFromDraft(w.dr)
		if patch.Terms == nil {
			patch.Terms = &service.TermsInput{Mode: "custom"}
		}
	default:
		w.stepError(ctx, expiredButtonMessage, false, false)
		return
	}
	updated, err := w.h.svc.UpdatePeriod(ctx, w.user.ID, p.ID, patch)
	if err != nil {
		hint := "保存失败：" + serviceMessage(err) + "\n草稿已保留。"
		if service.Code(err) == "conflict" {
			hint = "保存失败：有效期已被其他操作更新，本次未写入。\n草稿已保留。"
		}
		w.h.send(ctx, w.b, w.resp, hint)
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	rules, _ := w.h.svc.ListRules(ctx, w.user.ID, updated.ID, 0, 1)
	w.h.sendMarkup(ctx, w.b, w.resp, periodDetail(updated, len(rules.Data)), w.h.periodKeyboard(updated))
}

// ---- reminder rules -------------------------------------------------------

func (h *Handler) startRuleAdd(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, resp, "无法添加提醒规则："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, resp, "服务已归档，请恢复服务后再添加提醒规则。")
		return
	}
	if h.conflictTarget(ctx, b, user, resp, service.DialogRuleAdd, periodID, service.DialogRuleAdd+":"+id(periodID)) {
		return
	}
	serviceID := p.ServiceID
	d := service.Dialog{ServiceID: &serviceID, PeriodID: &periodID, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogRuleAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始提醒规则流程："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.dr.set("enabled", "true")
	if !w.persist(ctx, "rule_days") {
		return
	}
	w.renderRule(ctx)
}

func (h *Handler) startRuleEdit(ctx context.Context, b *telegram.Bot, user service.User, resp *responder, ruleID int64) {
	r, err := h.svc.GetRule(ctx, user.ID, ruleID)
	if err != nil {
		h.send(ctx, b, resp, "无法编辑规则："+serviceMessage(err))
		return
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, r.PeriodID)
	if err != nil {
		h.send(ctx, b, resp, "无法编辑规则："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, resp, "服务已归档，请恢复服务后再编辑规则。")
		return
	}
	if h.conflictTarget(ctx, b, user, resp, service.DialogRuleEdit, ruleID, service.DialogRuleEdit+":"+id(ruleID)) {
		return
	}
	revision := r.Revision
	d := service.Dialog{ServiceID: &p.ServiceID, PeriodID: &r.PeriodID, RuleID: &ruleID, ExpectedRevision: &revision, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogRuleEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始规则编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.dr.set("days_before", strconv.Itoa(r.DaysBefore))
	w.dr.set("reminder_time", r.ReminderTime)
	w.dr.set("enabled", strconv.FormatBool(r.Enabled))
	if !w.persist(ctx, "rule_days") {
		return
	}
	w.renderRule(ctx)
}

func (w *wizard) renderRule(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	head := "有效期「" + periodName(p) + "」\n截止：" + periodExpiry(p) + "\n"
	if w.d.Action == service.DialogRuleEdit && w.d.RuleID != nil {
		if r, err := w.h.svc.GetRule(ctx, w.user.ID, *w.d.RuleID); err == nil {
			head += ruleLinePlain(r) + "\n"
		}
	}
	switch w.d.Step {
	case "rule_days":
		w.reply(ctx, head+"\n请输入提前天数（大于等于 0 的整数），例如 7。", navRows(w.d.Action == service.DialogRuleEdit, false)...)
	case "rule_time":
		w.reply(ctx, "请输入提醒时间 HH:MM（24 小时制，时区 "+w.user.Timezone+"），例如 09:00。", navRows(true, false)...)
	case "rule_preview":
		toggle := button("改为禁用", cb("dialog", "toggle_enabled"))
		if w.dr.Values["enabled"] == "false" {
			toggle = button("改为启用", cb("dialog", "toggle_enabled"))
		}
		w.reply(ctx, rulePreviewText(p, w.dr), []models.InlineKeyboardButton{button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))}, []models.InlineKeyboardButton{toggle}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func rulePreviewText(p service.Period, d draft) string {
	return fmt.Sprintf("请确认提醒规则：\n有效期：%s\n截止：%s\n提前：%s 天\n提醒时间：%s\n启用：%s",
		periodName(p), periodExpiry(p), d.Values["days_before"], d.Values["reminder_time"], enabledLabel(d.Values["enabled"]))
}

func enabledLabel(value string) string {
	if value == "false" {
		return "否"
	}
	return "是"
}

func (w *wizard) textRule(ctx context.Context, text string) {
	value := trimmed(text)
	switch w.d.Step {
	case "rule_days":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 99999 {
			w.stepError(ctx, "提前天数必须是 0 到 99999 之间的整数。", w.d.Action == service.DialogRuleEdit, false)
			return
		}
		w.dr.set("days_before", strconv.Itoa(n))
	case "rule_time":
		if _, err := time.Parse("15:04", value); err != nil {
			w.stepError(ctx, "提醒时间必须是 HH:MM，例如 09:00。", true, false)
			return
		}
		w.dr.set("reminder_time", value)
	default:
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	next := nextStep(w.d.Action, w.dr, w.d.Step)
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func (w *wizard) confirmRule(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.PeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	days, err := strconv.Atoi(w.dr.Values["days_before"])
	if err != nil {
		w.stepError(ctx, expiredButtonMessage, false, false)
		return
	}
	enabled := w.dr.Values["enabled"] != "false"
	if w.d.Action == service.DialogRuleAdd {
		rule, err := w.h.svc.CreateRule(ctx, w.user.ID, p.ID, service.RuleInput{DaysBefore: days, ReminderTime: w.dr.Values["reminder_time"], Enabled: &enabled})
		if err != nil {
			w.h.send(ctx, w.b, w.resp, "创建规则失败："+serviceMessage(err)+"\n草稿已保留。")
			return
		}
		w.finishRule(ctx, rule, "提醒规则已创建。")
		return
	}
	if w.d.RuleID == nil || w.d.ExpectedRevision == nil {
		w.abort(ctx, "该流程已失效")
		return
	}
	reminder := w.dr.Values["reminder_time"]
	rule, err := w.h.svc.UpdateRule(ctx, w.user.ID, *w.d.RuleID, service.RulePatch{ExpectedRevision: *w.d.ExpectedRevision, DaysBefore: &days, ReminderTime: &reminder, Enabled: &enabled})
	if err != nil {
		hint := "保存规则失败：" + serviceMessage(err) + "\n草稿已保留。"
		if service.Code(err) == "conflict" {
			hint = "保存失败：规则已被修改，本次未写入。\n草稿已保留。"
		}
		w.h.send(ctx, w.b, w.resp, hint)
		return
	}
	w.finishRule(ctx, rule, "提醒规则已更新。")
}

func (w *wizard) finishRule(ctx context.Context, rule service.Rule, label string) {
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	text := label + "\n" + ruleLinePlain(rule)
	rows := [][]models.InlineKeyboardButton{
		[]models.InlineKeyboardButton{button("查看规则", cb("rule", "view", id(rule.ID))), button("编辑规则", cb("rule", "edit", id(rule.ID)))},
		[]models.InlineKeyboardButton{button("返回规则列表", cb("period", "rules", id(rule.PeriodID), "0"))},
	}
	w.h.sendMarkup(ctx, w.b, w.resp, text, markup(rows...))
}

func (h *Handler) startTimezone(ctx context.Context, b *telegram.Bot, user service.User, resp *responder) {
	if h.conflictTarget(ctx, b, user, resp, service.DialogTimezone, 0, service.DialogTimezone) {
		return
	}
	d := service.Dialog{Step: "tz_value", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, resp.chatID, service.DialogTimezone, d.Step, d)
	if err != nil {
		h.send(ctx, b, resp, "无法开始修改时区："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start, resp)
	w.renderTimezone(ctx)
}

var timezoneSamples = []string{"Asia/Taipei", "Asia/Tokyo", "Asia/Shanghai", "Europe/London", "America/New_York", "UTC"}

func (w *wizard) renderTimezone(ctx context.Context) {
	switch w.d.Step {
	case "tz_value":
		rows := [][]models.InlineKeyboardButton{}
		row := []models.InlineKeyboardButton{}
		for _, zone := range timezoneSamples {
			row = append(row, button(zone, choiceCallback("tz", zone)))
			if len(row) == 2 {
				rows = append(rows, row)
				row = []models.InlineKeyboardButton{}
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
		rows = append(rows, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
		w.reply(ctx, "当前时区："+w.user.Timezone+"\n请选择常用时区，或直接发送合法 IANA 名称（例如 Asia/Taipei）。", rows...)
	case "tz_preview":
		w.reply(ctx, fmt.Sprintf("请确认修改时区：\n旧时区：%s\n新时区：%s\n\n仅日期截止会按新时区重算当日结束；精确时间保持绝对时刻。未发送的提醒会取消。",
			w.user.Timezone, w.dr.Values["tz"]), []models.InlineKeyboardButton{button("确认修改", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))}, []models.InlineKeyboardButton{button("取消", cb("dialog", "cancel"))})
	default:
		w.abort(ctx, "该流程已失效")
	}
}

func (w *wizard) textTimezone(ctx context.Context, text string) {
	if w.d.Step != "tz_value" {
		w.stepError(ctx, "请用下方按钮选择。", false, false)
		return
	}
	zone := trimmed(text)
	if zone == "" || zone == "Local" {
		w.stepError(ctx, "时区必须是合法 IANA 名称，例如 Asia/Taipei。", false, false)
		return
	}
	if _, err := time.LoadLocation(zone); err != nil {
		w.stepError(ctx, "时区必须是合法 IANA 名称，例如 Asia/Taipei。", false, false)
		return
	}
	w.dr.set("tz", zone)
	if !w.persist(ctx, "tz_preview") {
		return
	}
	w.render(ctx)
}

func (w *wizard) confirmTimezone(ctx context.Context) {
	zone := w.dr.Values["tz"]
	user, err := w.h.svc.SetTimezone(ctx, w.user.ID, zone)
	if err != nil {
		w.h.send(ctx, w.b, w.resp, "修改时区失败："+serviceMessage(err)+"\n草稿已保留。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.sendMarkup(ctx, w.b, w.resp, "时区已更新为 "+user.Timezone+"。\n仅日期截止已按新时区重算，精确时间的绝对时刻不变。",
		markup([]models.InlineKeyboardButton{button("返回设置", cb("settings", "view"))}))
}
