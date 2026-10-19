package bot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/service"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ---- service add -----------------------------------------------------------

func (h *Handler) startServiceAdd(ctx context.Context, b *telegram.Bot, user service.User, chatID int64) {
	if h.conflictTarget(ctx, b, user, chatID, service.DialogServiceAdd, 0, service.DialogServiceAdd) {
		return
	}
	d := service.Dialog{Step: "add_category", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogServiceAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始添加服务："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
		w.reply(ctx, "添加服务 · 第 1 步，选择类别。\n只会列出你可使用的内置类别与本人自定义类别；也可发送 /cancel 取消。", rows...)
	case "add_category_name":
		w.reply(ctx, "输入新类别名称（最多 200 字）。创建后会回到类别选择。", navRows(true, false)...)
	case "add_name":
		w.reply(ctx, "第 2 步，输入服务名称（例如：东京 VPS）。", navRows(true, false)...)
	case "add_card_type":
		w.reply(ctx, "这是 SIM 类别，请选择卡类型。", pair(
			button("实体卡", choiceCallback("card_type", "physical_sim")),
			button("eSIM", choiceCallback("card_type", "esim")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "add_provider":
		w.reply(ctx, "可选：服务商/运营商（例如 Carrier、Cloud Provider）。", optionalNav(true)...)
	case "add_identifier":
		w.reply(ctx, "可选：公开标识（主机名、域名、账号标识或公开号码；不要填密码）。", optionalNav(true)...)
	case "add_management_url":
		w.reply(ctx, "可选：管理链接（必须是无内嵌凭据的 http(s) 绝对地址）。", optionalNav(true)...)
	case "add_phone":
		w.reply(ctx, "可选：号码。会保存在数据库，不会进入回调或日志。", optionalNav(true)...)
	case "add_region":
		w.reply(ctx, "可选：国家或地区。", optionalNav(true)...)
	case "add_iccid":
		w.reply(ctx, "可选：ICCID（敏感信息不进入回调或日志）。", optionalNav(true)...)
	case "add_notes":
		w.reply(ctx, "可选：普通备注（最多 4000 字；不要保存密码、API Token 或私钥）。", optionalNav(true)...)
	case "add_preview":
		w.renderServiceAddPreview(ctx)
	default:
		w.abort(ctx, "添加服务流程状态无效")
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
	fmt.Fprintf(&b, "请确认新建服务：\n类别：%s\n名称：%s\n服务商：%s\n公开标识：%s\n管理链接：%s\n备注：%s\n",
		categoryName, w.dr.Values["name"], shown(w.dr, "provider"), shown(w.dr, "identifier"), shown(w.dr, "management_url"), shown(w.dr, "notes"))
	if kind == "sim" {
		fmt.Fprintf(&b, "卡类型：%s\n号码：%s\n国家/地区：%s\nICCID：%s\n", cardTypeLabel(w.dr.Values["card_type"]), shown(w.dr, "phone"), shown(w.dr, "region"), shown(w.dr, "iccid"))
	}
	b.WriteString("\n确认后只创建这一个服务；不会自动创建有效期或提醒规则。")
	w.reply(ctx, b.String(), pair(button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
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
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				w.stepError(ctx, "管理链接必须是无内嵌凭据的绝对 http(s) 地址。", true, true)
				return
			}
		}
		w.dr.set("management_url", value)
	default:
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
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
		w.stepError(ctx, "类别或名称缺失，请返回修改。", false, false)
		return
	}
	in := service.ServiceInput{Name: w.dr.Values["name"], CategoryID: categoryID,
		Provider: opt(w.dr, "provider"), Identifier: opt(w.dr, "identifier"),
		ManagementURL: opt(w.dr, "management_url"), Notes: opt(w.dr, "notes")}
	if w.dr.Values["kind"] == "sim" {
		in.SIMDetails = &service.SIMDetails{CardType: w.dr.Values["card_type"], PhoneNumber: opt(w.dr, "phone"),
			CountryOrRegion: opt(w.dr, "region"), ICCID: opt(w.dr, "iccid")}
	} else {
		for _, key := range []string{"card_type", "phone", "region", "iccid"} {
			w.dr.del(key)
		}
	}
	item, err := w.h.svc.CreateService(ctx, w.user.ID, in)
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "创建失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.showServiceDetail(ctx, w.b, w.user, w.chatID, item.ID, 0)
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
		pair(button("新建类别…", cb("dialog", "new_category"))),
		pair(button("取消", cb("dialog", "cancel"))),
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

func (h *Handler) startServiceEdit(ctx context.Context, b *telegram.Bot, user service.User, chatID, serviceID int64) {
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑："+serviceMessage(err))
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogServiceEdit, serviceID, service.DialogServiceEdit+":"+id(serviceID)) {
		return
	}
	d := service.Dialog{ServiceID: &serviceID, Step: "edit_field", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogServiceEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
	switch w.d.Step {
	case "edit_field":
		w.reply(ctx, "编辑服务「"+item.Name+"」\n请选择要修改的字段；每次只提交一项，确认前不会写入。", editFieldRows(item)...)
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
			rows = append(rows, pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
			w.reply(ctx, "当前类别："+editValue(item, field)+"\n请选择新的类别（只能在同类 kind 内切换）。", rows...)
			return
		}
		if field == "card_type" {
			w.reply(ctx, "当前卡类型："+editValue(item, field), pair(
				button("实体卡", choiceCallback("card_type", "physical_sim")),
				button("eSIM", choiceCallback("card_type", "esim")),
			), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
			return
		}
		rows := navRows(true, false)
		if field != "name" {
			rows = append([][]models.InlineKeyboardButton{pair(button("清空当前值", cb("dialog", "clear_value")))}, rows...)
		}
		w.reply(ctx, fmt.Sprintf("当前值：%s\n\n请输入新的%s；名称不能清空。", editValue(item, field), editLabel(field)), rows...)
	case "edit_preview":
		field := w.dr.Values["field"]
		w.reply(ctx, fmt.Sprintf("请确认修改服务「%s」：\n字段：%s\n旧值：%s\n新值：%s",
			item.Name, editLabel(field), editValue(item, field), draftEditValue(w.dr, field)),
			pair(button("确认修改", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))),
			pair(button("改其他字段", cb("dialog", "change_field")), button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "编辑流程状态无效")
	}
}

func editLabel(field string) string {
	return map[string]string{
		"name": "名称", "provider": "服务商", "identifier": "公开标识", "management_url": "管理链接",
		"notes": "备注", "card_type": "卡类型", "phone": "号码", "region": "国家/地区", "iccid": "ICCID",
		"category_id": "类别",
	}[field]
}

func editValue(item service.ServiceItem, field string) string {
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
		return "当前类别"
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
	fields := [][2]string{{"name", "名称"}, {"category_id", "类别"}, {"provider", "服务商"}, {"identifier", "公开标识"}, {"management_url", "管理链接"}, {"notes", "备注"}}
	if item.SIMDetails != nil {
		fields = append(fields, [2]string{"card_type", "SIM 卡类型"}, [2]string{"phone", "号码"}, [2]string{"region", "国家/地区"}, [2]string{"iccid", "ICCID"})
	}
	rows := [][]models.InlineKeyboardButton{}
	row := []models.InlineKeyboardButton{}
	for _, f := range fields {
		row = append(row, button(f[1], choiceCallback("field", f[0])))
		if len(row) == 2 {
			rows = append(rows, row)
			row = []models.InlineKeyboardButton{}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, pair(button("返回服务", cb("service", "view", id(item.ID), "0")), button("取消", cb("dialog", "cancel"))))
	return rows
}

func (w *wizard) textServiceEdit(ctx context.Context, text string) {
	if w.d.Step != "edit_value" {
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
		return
	}
	field, value := w.dr.Values["field"], trimmed(text)
	if field == "category_id" || field == "card_type" {
		w.stepError(ctx, "请使用下方按钮选择。", true, false)
		return
	}
	switch field {
	case "name":
		if value == "" || len([]rune(value)) > 200 {
			w.stepError(ctx, "名称必填且不超过 200 字。", true, false)
			return
		}
	case "management_url":
		if value != "" {
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				w.stepError(ctx, "管理链接必须是无内嵌凭据的绝对 http(s) 地址。", true, false)
				return
			}
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
	set := func(dest *service.Optional[string]) {
		*dest = service.Optional[string]{Set: true, Value: nullable(value)}
	}
	switch field {
	case "name":
		patch.Name = &value
	case "category_id":
		n, err := strconv.ParseInt(w.dr.Values["category_id"], 10, 64)
		if err != nil || n < 1 {
			w.stepError(ctx, "类别无效，请返回修改。", false, false)
			return
		}
		patch.CategoryID = &n
	case "provider":
		set(&patch.Provider)
	case "identifier":
		set(&patch.Identifier)
	case "management_url":
		set(&patch.ManagementURL)
	case "notes":
		set(&patch.Notes)
	case "card_type":
		patch.SIMDetails = service.Optional[service.SIMDetailsPatch]{Set: true, Value: &service.SIMDetailsPatch{CardType: service.Optional[string]{Set: true, Value: &value}}}
	case "phone", "region", "iccid":
		details := service.SIMDetailsPatch{}
		switch field {
		case "phone":
			details.PhoneNumber = service.Optional[string]{Set: true, Value: nullable(value)}
		case "region":
			details.CountryOrRegion = service.Optional[string]{Set: true, Value: nullable(value)}
		case "iccid":
			details.ICCID = service.Optional[string]{Set: true, Value: nullable(value)}
		}
		patch.SIMDetails = service.Optional[service.SIMDetailsPatch]{Set: true, Value: &details}
	default:
		w.stepError(ctx, "未知字段，请重新打开编辑。", false, false)
		return
	}
	item, err := w.h.svc.UpdateService(ctx, w.user.ID, *w.d.ServiceID, patch)
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "修改失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.showServiceDetail(ctx, w.b, w.user, w.chatID, item.ID, 0)
}

// ---- period add -----------------------------------------------------------

func (h *Handler) startPeriodAdd(ctx context.Context, b *telegram.Bot, user service.User, chatID, serviceID int64) {
	item, err := h.svc.GetService(ctx, user.ID, serviceID)
	if err != nil {
		h.send(ctx, b, chatID, "无法添加有效期："+serviceMessage(err))
		return
	}
	if item.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请先恢复服务再添加有效期。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogPeriodAdd, serviceID, service.DialogPeriodAdd+":"+id(serviceID)) {
		return
	}
	d := service.Dialog{ServiceID: &serviceID, Step: "period_name", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogPeriodAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始添加有效期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
		w.reply(ctx, fmt.Sprintf("添加有效期到「%s」· 第 1 步，有效期名称（可选，例如：年付套餐）。", item.Name), optionalNav(true)...)
	case "period_precision":
		w.reply(ctx, "第 2 步，截止方式。\n仅日期表示当日结束前有效；精确时间保持绝对时刻。", pair(
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "period_expires":
		w.reply(ctx, "第 3 步，输入截止（"+precisionLabel(w.dr.Values["precision"])+"，时区 "+w.user.Timezone+"）。\n"+expirationHint(w.dr.Values["precision"]), navRows(true, false)...)
	case "period_starts":
		w.reply(ctx, "第 4 步，开始时间（可选，按你的时区，必须早于截止）。\n示例：2027-01-01 00:00", optionalNav(true)...)
	case "terms_mode":
		w.reply(ctx, "第 5 步，续费约定（可选，只作记录，不会自动扣款或推算日期）。", pair(
			button("一次性", choiceCallback("mode", "once")),
			button("周期性", choiceCallback("mode", "recurring")),
			button("暂不设置", choiceCallback("mode", "custom")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "terms_interval":
		w.reply(ctx, "周期长度，例如：1 month、2 week。单位可为 day/week/month/year。", navRows(true, false)...)
	case "terms_amount":
		w.reply(ctx, "约定金额（可选）：发送“金额 币种”，例如 120000 JPY；金额为最小货币单位整数。\n约定费用不代表已付款。", optionalNav(true)...)
	case "period_preview":
		w.reply(ctx, periodAddPreviewText(item, w.dr, w.user.Timezone), pair(button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "添加有效期流程状态无效")
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
	return fmt.Sprintf("请确认新建有效期：\n服务：%s\n名称：%s\n截止方式：%s\n截止：%s\n开始：%s\n约定：%s\n\n创建后不会自动创建提醒规则。",
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
		if err != nil || amount == nil {
			w.stepError(ctx, "请发送“金额 币种”，例如 2999 USD；金额必须是非负整数。", true, true)
			return
		}
		w.dr.set("amount", strconv.FormatInt(*amount, 10))
		w.dr.set("currency", *currency)
	default:
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
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
		w.h.send(ctx, w.b, w.chatID, "创建有效期失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	rules, _ := w.h.svc.ListRules(ctx, w.user.ID, p.ID, 0, 1)
	w.h.sendMarkup(ctx, w.b, w.chatID, periodDetail(p, len(rules.Data)), markup(
		pair(button("设置提醒", cb("rule", "add", id(p.ID))), button("立即续期", cb("period", "renew", id(p.ID)))),
		pair(button("查看有效期", cb("period", "view", id(p.ID))), button("返回服务", cb("service", "view", id(p.ServiceID), "0"))),
	))
}

// ---- period edit ----------------------------------------------------------

func (h *Handler) startPeriodEdit(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑有效期："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请恢复服务后再编辑有效期。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogPeriodEdit, periodID, service.DialogPeriodEdit+":"+id(periodID)) {
		return
	}
	cycle, revision := p.Cycle, p.Terms.Revision
	serviceID := p.ServiceID
	d := service.Dialog{ServiceID: &serviceID, PeriodID: &periodID, ExpectedCycle: &cycle, ExpectedTermsRevision: &revision, Step: "edit_field", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogPeriodEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始编辑有效期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
		rows := [][]models.InlineKeyboardButton{
			pair(button("名称", choiceCallback("pfield", "name")), button("截止", choiceCallback("pfield", "expires"))),
			pair(button("开始时间", choiceCallback("pfield", "starts")), button("约定条款", choiceCallback("pfield", "terms"))),
			pair(button("返回有效期", cb("period", "view", id(p.ID))), button("取消", cb("dialog", "cancel"))),
		}
		w.reply(ctx, "编辑有效期「"+periodName(p)+"」\n请选择要修改的字段；确认前不会写入。", rows...)
	case "edit_value":
		field := w.dr.Values["field"]
		if field == "name" {
			w.reply(ctx, "当前名称："+shownValue(p.Name)+"\n请输入新名称；发送空内容表示不设置名称。", navRows(true, false)...)
		} else {
			rows := navRows(true, false)
			if p.StartsAt != nil {
				rows = append([][]models.InlineKeyboardButton{pair(button("清除开始时间", cb("dialog", "clear_value")))}, rows...)
			}
			w.reply(ctx, "当前开始："+startValue(p.StartsAt, p.Timezone)+"\n请输入本地时间，例如 2027-01-01 00:00（按 "+p.Timezone+"）；也接受带 offset 的 RFC3339。", rows...)
		}
	case "period_precision":
		w.reply(ctx, "选择新的截止方式。\n仅日期表示当日结束前有效；精确时间保持绝对时刻。", pair(
			button("仅日期", choiceCallback("precision", "date")),
			button("精确时间", choiceCallback("precision", "datetime")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "period_expires":
		w.reply(ctx, "当前截止："+periodExpiry(p)+"\n请输入新的截止（"+precisionLabel(w.dr.Values["precision"])+"，时区 "+p.Timezone+"）。\n"+expirationHint(w.dr.Values["precision"]), navRows(true, false)...)
	case "terms_mode":
		w.reply(ctx, "当前约定："+termsText(p.Terms)+"\n请选择新的条款模式；条款只作记录，不会自动扣款或改变截止。", pair(
			button("一次性", choiceCallback("mode", "once")),
			button("周期性", choiceCallback("mode", "recurring")),
			button("暂不设置", choiceCallback("mode", "custom")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "terms_interval":
		w.reply(ctx, "周期长度，例如：1 month、2 week。单位可为 day/week/month/year。", navRows(true, false)...)
	case "terms_amount":
		w.reply(ctx, "约定金额（可选）：发送“金额 币种”，例如 120000 JPY；发送“无”或跳过表示不记录金额。", optionalNav(true)...)
	case "edit_preview":
		w.reply(ctx, periodEditPreviewText(p, w.dr), pair(button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "编辑有效期流程状态无效")
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
	return fmt.Sprintf("请确认修改有效期「%s」：\n字段：%s\n旧值：%s\n新值：%s\n\n保存时会按当前版本校验；若已被其他操作更新会拒绝并保留草稿。",
		periodName(p), periodFieldLabel(field), old, next)
}

func periodFieldLabel(field string) string {
	switch field {
	case "name":
		return "名称"
	case "expires":
		return "截止"
	case "starts":
		return "开始时间"
	case "terms":
		return "约定条款"
	}
	return field
}

func (w *wizard) textPeriodEdit(ctx context.Context, text string) {
	value := trimmed(text)
	loadPeriod := func() (service.Period, bool) {
		if w.d.PeriodID == nil {
			w.abort(ctx, "编辑流程缺少有效期")
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
		if err != nil || amount == nil {
			w.stepError(ctx, "请发送“金额 币种”，例如 2999 USD；金额必须是非负整数。", true, true)
			return
		}
		w.dr.set("amount", strconv.FormatInt(*amount, 10))
		w.dr.set("currency", *currency)
	default:
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
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
		w.h.send(ctx, w.b, w.chatID, "有效期已被其他操作更新，本次未写入；草稿仍保留。请取消后重新打开有效期查看最新状态。")
		return
	}
	field := w.dr.Values["field"]
	patch := service.PeriodPatch{ExpectedCycle: *w.d.ExpectedCycle}
	switch field {
	case "name":
		value := w.dr.Values["value"]
		patch.Name = &value
	case "starts":
		patch.StartsAt = service.Optional[string]{Set: true, Value: nullable(w.dr.Values["value"])}
	case "expires":
		precision := w.dr.Values["precision"]
		expires := w.dr.Values["expires_at"]
		patch.DatePrecision, patch.ExpiresAt = &precision, &expires
	case "terms":
		if w.d.ExpectedTermsRevision == nil {
			w.abort(ctx, "条款对话缺少版本信息")
			return
		}
		patch.ExpectedTermsRevision = w.d.ExpectedTermsRevision
		patch.Terms = service.Optional[service.TermsInput]{Set: true, Value: termsFromDraft(w.dr)}
		if patch.Terms.Value == nil {
			patch.Terms.Value = &service.TermsInput{Mode: "custom"}
		}
	default:
		w.stepError(ctx, "未知字段，请重新打开编辑。", false, false)
		return
	}
	updated, err := w.h.svc.UpdatePeriod(ctx, w.user.ID, p.ID, patch)
	if err != nil {
		hint := "保存失败：" + serviceMessage(err) + "\n草稿仍保留，可返回修改或取消。"
		if service.Code(err) == "conflict" {
			hint = "保存失败：有效期已被其他操作更新，本次未写入。\n草稿仍保留；可取消后重新打开有效期查看最新状态。"
		}
		w.h.send(ctx, w.b, w.chatID, hint)
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	rules, _ := w.h.svc.ListRules(ctx, w.user.ID, updated.ID, 0, 1)
	w.h.sendMarkup(ctx, w.b, w.chatID, periodDetail(updated, len(rules.Data)), w.h.periodKeyboard(updated))
}

// ---- reminder rules -------------------------------------------------------

func (h *Handler) startRuleAdd(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法添加提醒规则："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请恢复服务后再添加提醒规则。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRuleAdd, periodID, service.DialogRuleAdd+":"+id(periodID)) {
		return
	}
	serviceID := p.ServiceID
	d := service.Dialog{ServiceID: &serviceID, PeriodID: &periodID, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogRuleAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始提醒规则流程："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
	w.dr.set("enabled", "true")
	if !w.persist(ctx, "rule_days") {
		return
	}
	w.renderRule(ctx)
}

func (h *Handler) startRuleEdit(ctx context.Context, b *telegram.Bot, user service.User, chatID, ruleID int64) {
	r, err := h.svc.GetRule(ctx, user.ID, ruleID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑规则："+serviceMessage(err))
		return
	}
	p, err := h.svc.GetPeriod(ctx, user.ID, r.PeriodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑规则："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID)
	if err != nil || parent.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请恢复服务后再编辑规则。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRuleEdit, ruleID, service.DialogRuleEdit+":"+id(ruleID)) {
		return
	}
	revision := r.Revision
	d := service.Dialog{ServiceID: &p.ServiceID, PeriodID: &r.PeriodID, RuleID: &ruleID, ExpectedRevision: &revision, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogRuleEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始规则编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
	case "rule_enabled":
		w.reply(ctx, "是否启用该规则？", pair(button("启用", choiceCallback("enabled", "true")), button("禁用", choiceCallback("enabled", "false"))),
			pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "rule_preview":
		w.reply(ctx, rulePreviewText(p, w.dr), pair(button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "提醒规则流程状态无效")
	}
}

func rulePreviewText(p service.Period, d draft) string {
	return fmt.Sprintf("请确认提醒规则：\n有效期：%s\n截止：%s\n提前：%s 天\n提醒时间：%s\n启用：%s\n\n保存时由服务层重新校验提醒时间是否早于截止。",
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
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
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
		w.stepError(ctx, "提前天数无效，请返回修改。", false, false)
		return
	}
	enabled := w.dr.Values["enabled"] != "false"
	if w.d.Action == service.DialogRuleAdd {
		rule, err := w.h.svc.CreateRule(ctx, w.user.ID, p.ID, service.RuleInput{DaysBefore: days, ReminderTime: w.dr.Values["reminder_time"], Enabled: &enabled})
		if err != nil {
			w.h.send(ctx, w.b, w.chatID, "创建规则失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
			return
		}
		w.finishRule(ctx, rule, "提醒规则已创建。")
		return
	}
	if w.d.RuleID == nil || w.d.ExpectedRevision == nil {
		w.abort(ctx, "规则对话缺少版本信息")
		return
	}
	reminder := w.dr.Values["reminder_time"]
	rule, err := w.h.svc.UpdateRule(ctx, w.user.ID, *w.d.RuleID, service.RulePatch{ExpectedRevision: *w.d.ExpectedRevision, DaysBefore: &days, ReminderTime: &reminder, Enabled: &enabled})
	if err != nil {
		hint := "保存规则失败：" + serviceMessage(err) + "\n草稿仍保留，可返回修改或取消。"
		if service.Code(err) == "conflict" {
			hint = "保存失败：规则已被修改，请取消后重新打开规则列表。\n草稿仍保留。"
		}
		w.h.send(ctx, w.b, w.chatID, hint)
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
		pair(button("查看规则", cb("rule", "view", id(rule.ID))), button("编辑规则", cb("rule", "edit", id(rule.ID)))),
		pair(button("返回规则列表", cb("period", "rules", id(rule.PeriodID), "0"))),
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, text, markup(rows...))
}

func (h *Handler) startTimezone(ctx context.Context, b *telegram.Bot, user service.User, chatID int64) {
	if h.conflictTarget(ctx, b, user, chatID, service.DialogTimezone, 0, service.DialogTimezone) {
		return
	}
	d := service.Dialog{Step: "tz_value", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogTimezone, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始修改时区："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
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
		rows = append(rows, pair(button("取消", cb("dialog", "cancel"))))
		w.reply(ctx, "当前时区："+w.user.Timezone+"\n请选择常用时区，或直接发送合法 IANA 名称（例如 Asia/Taipei）。", rows...)
	case "tz_preview":
		w.reply(ctx, fmt.Sprintf("请确认修改时区：\n旧时区：%s\n新时区：%s\n\n仅日期截止会按新时区重算当地日历日结束；精确时间保持绝对时刻。相关有效期会推进版本并取消未完成提醒任务。",
			w.user.Timezone, w.dr.Values["tz"]), pair(button("确认修改", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "时区流程状态无效")
	}
}

func (w *wizard) textTimezone(ctx context.Context, text string) {
	if w.d.Step != "tz_value" {
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
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
		w.h.send(ctx, w.b, w.chatID, "修改时区失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, "时区已更新为 "+user.Timezone+"。\n仅日期截止已按新时区重算，精确时间的绝对时刻不变。",
		markup(pair(button("返回设置", cb("settings", "view")))))
}
