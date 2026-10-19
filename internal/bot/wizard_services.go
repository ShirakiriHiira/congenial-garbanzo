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
	if h.conflictTarget(ctx, b, user, chatID, service.DialogServiceAdd, 0, "service_add") {
		return
	}
	d := service.Dialog{Step: "add_category", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogServiceAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始添加服务："+serviceMessage(err))
		return
	}
	{
		w := h.wizard(ctx, b, user, start)
		w.renderServiceAdd(ctx)
	}
}

func (w *wizard) renderServiceAdd(ctx context.Context) {
	switch w.d.Step {
	case "add_category":
		rows, err := categoryRows(ctx, w.h, w.user.ID)
		if err != nil {
			w.abort(ctx, "读取类别失败")
			return
		}
		w.reply(ctx, "添加服务：第 1 步，选择类别。\n只会列出你可使用的内置类别与本人自定义类别；也可发送 /cancel 取消。", rows...)
	case "add_name":
		w.reply(ctx, "请输入服务名称（必填，最多 200 字）。\n例如：日本 eSIM 或 VPS Pro", navRows(true, false)...)
	case "add_provider":
		w.reply(ctx, "服务商/运营商（可选）。可跳过。", navRows(true, true)...)
	case "add_identifier":
		w.reply(ctx, "公开标识（可选，例如账号名或主机名；不要填密码）。可跳过。", navRows(true, true)...)
	case "add_management_url":
		w.reply(ctx, "管理链接（可选，必须是以 http:// 或 https:// 开头的绝对地址，且不能内嵌账号密码）。可跳过。", navRows(true, true)...)
	case "add_notes":
		w.reply(ctx, "普通备注（可选，最多 4000 字；不要保存密码、API Token 或私钥）。可跳过。", navRows(true, true)...)
	case "add_card_type":
		w.reply(ctx, "这是 SIM 类别，请选择卡类型。", pair(
			button("实体卡 physical_sim", choiceCallback("card_type", "physical_sim")),
			button("eSIM", choiceCallback("card_type", "esim")),
		), pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "add_phone":
		w.reply(ctx, "手机号（可选）。会保存在数据库，不会进入回调或日志。可跳过。", navRows(true, true)...)
	case "add_region":
		w.reply(ctx, "国家或地区（可选）。可跳过。", navRows(true, true)...)
	case "add_iccid":
		w.reply(ctx, "ICCID（可选，敏感信息不进入回调或日志）。可跳过。", navRows(true, true)...)
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
	w.dr.set("kind", kind)
	var b strings.Builder
	fmt.Fprintf(&b, "请确认新建服务：\n类别：%s\n名称：%s\n服务商：%s\n公开标识：%s\n管理链接：%s\n备注：%s\n",
		categoryName, w.dr.Values["name"], shown(w.dr, "provider"), shown(w.dr, "identifier"), shown(w.dr, "management_url"), shown(w.dr, "notes"))
	if kind == "sim" {
		fmt.Fprintf(&b, "卡类型：%s\n电话：%s\n国家/地区：%s\nICCID：%s\n", w.dr.Values["card_type"], shown(w.dr, "phone"), shown(w.dr, "region"), shown(w.dr, "iccid"))
	}
	b.WriteString("\n确认后只创建这一个服务；不会自动创建有效期或提醒规则。")
	w.reply(ctx, b.String(), pair(button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
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
		// Flat SIM input must never be attached to a generic category.
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
	detail, page, err := w.h.serviceDetailPage(ctx, w.user.ID, item, 0)
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "服务已创建，但读取详情失败。")
		return
	}
	rows := [][]models.InlineKeyboardButton{
		pair(button("查看服务", cb("service", "view", id(item.ID))), button("添加有效期", cb("period", "add", id(item.ID)))),
	}
	if kb := w.h.serviceKeyboard(item, &page); kb != nil {
		rows = append(rows, kb.InlineKeyboard...)
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, detail, markup(rows...))
}

func opt(d draft, key string) *string {
	v, ok := d.get(key)
	if !ok || strings.TrimSpace(v) == "" {
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
	rows = append(rows, pair(button("取消", cb("dialog", "cancel"))))
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
	if h.conflictTarget(ctx, b, user, chatID, service.DialogServiceEdit, serviceID, "service_edit:"+id(serviceID)) {
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
	w.persist(ctx, "edit_field")
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
	if w.d.Step == "edit_field" {
		rows := editFieldRows(item)
		w.reply(ctx, fmt.Sprintf("编辑服务 #%s %s\n请选择要修改的字段；每次只提交一项，确认前不会写入。", id(item.ID), item.Name), rows...)
		return
	}
	if w.d.Step == "edit_value" {
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
		w.reply(ctx, fmt.Sprintf("当前值：%s\n\n请输入新的 %s；发送空内容表示清空（名称与卡类型不能清空）。", editValue(item, field), editLabel(field)), navRows(true, false)...)
		return
	}
	if w.d.Step == "edit_preview" {
		field := w.dr.Values["field"]
		w.reply(ctx, fmt.Sprintf("请确认修改服务 #%s %s：\n字段：%s\n旧值：%s\n新值：%s\n\n提交时会重新读取对象并复用服务层校验；服务资料本身没有统一版本号。",
			id(item.ID), item.Name, editLabel(field), editValue(item, field), draftEditValue(w.dr, field)),
			pair(button("确认修改", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))),
			pair(button("改其他字段", cb("dialog", "change_field")), button("取消", cb("dialog", "cancel"))))
		return
	}
	w.abort(ctx, "编辑流程状态无效")
}

func editLabel(field string) string {
	return map[string]string{
		"name": "名称", "provider": "服务商", "identifier": "公开标识", "management_url": "管理链接",
		"notes": "备注", "card_type": "卡类型", "phone": "电话", "region": "国家/地区", "iccid": "ICCID",
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
		return "#" + id(item.CategoryID)
	}
	if d := item.SIMDetails; d != nil {
		switch field {
		case "card_type":
			return d.CardType
		case "phone":
			return textOrDash(d.PhoneNumber)
		case "region":
			return textOrDash(d.CountryOrRegion)
		case "iccid":
			return textOrDash(d.ICCID)
		}
	}
	return "-"
}

func editFieldRows(item service.ServiceItem) [][]models.InlineKeyboardButton {
	fields := [][2]string{{"name", "名称"}, {"category_id", "类别"}, {"provider", "服务商"}, {"identifier", "公开标识"}, {"management_url", "管理链接"}, {"notes", "备注"}}
	if item.SIMDetails != nil {
		fields = append(fields, [2]string{"card_type", "SIM 卡类型"}, [2]string{"phone", "电话"}, [2]string{"region", "国家/地区"}, [2]string{"iccid", "ICCID"})
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
	rows = append(rows, pair(button("返回服务", cb("service", "view", id(item.ID))), button("取消", cb("dialog", "cancel"))))
	return rows
}

func (w *wizard) textServiceEdit(ctx context.Context, text string) {
	if w.d.Step != "edit_value" {
		w.stepError(ctx, "当前步骤不接受文本输入，请使用下方按钮。", false, false)
		return
	}
	field, value := w.dr.Values["field"], trimmed(text)
	if field == "category_id" {
		w.stepError(ctx, "请使用下方按钮选择类别。", true, false)
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
	case "card_type":
		if value != "esim" && value != "physical_sim" {
			w.stepError(ctx, "卡类型只能是 esim 或 physical_sim，且不能清空。", true, false)
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
	if field == "category_id" {
		if v := d.Values["category_id"]; v != "" {
			return "#" + v
		}
		return "（未填写）"
	}
	return shown(d, "value")
}

func (w *wizard) confirmServiceEdit(ctx context.Context) {
	field, value := w.dr.Values["field"], w.dr.Values["value"]
	if field == "category_id" {
		value = w.dr.Values["category_id"]
	}
	patch := service.ServicePatch{}
	set := func(dest *service.Optional[string]) {
		var v *string
		if value != "" {
			v = &value
		}
		*dest = service.Optional[string]{Set: true, Value: v}
	}
	switch field {
	case "name":
		patch.Name = &value
	case "category_id":
		n, err := strconv.ParseInt(value, 10, 64)
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
		set(&details.PhoneNumber)
		set(&details.CountryOrRegion)
		set(&details.ICCID)
		switch field {
		case "phone":
			details.PhoneNumber = service.Optional[string]{Set: true, Value: nullable(value)}
			details.CountryOrRegion, details.ICCID = service.Optional[string]{}, service.Optional[string]{}
		case "region":
			details.CountryOrRegion = service.Optional[string]{Set: true, Value: nullable(value)}
			details.PhoneNumber, details.ICCID = service.Optional[string]{}, service.Optional[string]{}
		case "iccid":
			details.ICCID = service.Optional[string]{Set: true, Value: nullable(value)}
			details.PhoneNumber, details.CountryOrRegion = service.Optional[string]{}, service.Optional[string]{}
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
	detail, page, err := w.h.serviceDetailPage(ctx, w.user.ID, item, 0)
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "服务已修改，但读取详情失败。")
		return
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, detail, w.h.serviceKeyboard(item, &page))
}

func nullable(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
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
	if h.conflictTarget(ctx, b, user, chatID, service.DialogPeriodAdd, serviceID, "period_add:"+id(serviceID)) {
		return
	}
	d := service.Dialog{ServiceID: &serviceID, Step: "period_type", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogPeriodAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始添加有效期："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
	w.dr.set("kind", kindOf(item))
	w.persist(ctx, "period_type")
	w.renderPeriodAdd(ctx)
}

func (w *wizard) renderPeriodAdd(ctx context.Context) {
	item, err := w.h.svc.GetService(ctx, w.user.ID, *w.d.ServiceID)
	if err != nil {
		w.abort(ctx, "服务已不存在")
		return
	}
	switch w.d.Step {
	case "period_type":
		labels := [][2]string{{"custom", "自定义 custom"}}
		if item.SIMDetails != nil {
			labels = [][2]string{{"number_validity", "号码有效期"}, {"plan_validity", "套餐有效期"},
				{"data_package_validity", "数据包有效期"}, {"custom", "自定义 custom"}}
		}
		rows := [][]models.InlineKeyboardButton{}
		row := []models.InlineKeyboardButton{}
		for _, l := range labels {
			row = append(row, button(l[1], choiceCallback("period_type", l[0])))
			if len(row) == 2 {
				rows = append(rows, row)
				row = []models.InlineKeyboardButton{}
			}
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
		rows = append(rows, pair(button("取消", cb("dialog", "cancel"))))
		note := "该服务不是 SIM，只能创建 custom 有效期。"
		if item.SIMDetails != nil {
			note = "该服务是 SIM，可选择四种有效期类型。"
		}
		w.reply(ctx, "添加有效期到服务 #"+id(item.ID)+" "+item.Name+"\n"+note, rows...)
	case "period_name":
		w.reply(ctx, "有效期名称（可选，最多 200 字），可跳过。", navRows(true, true)...)
	case "period_precision":
		w.reply(ctx, "截止时间使用哪种精度？\ndate 表示当地日历日结束前有效；datetime 表示精确到时刻且不随用户时区重算。",
			pair(button("仅日期 date", choiceCallback("precision", "date")), button("日期时间 datetime", choiceCallback("precision", "datetime"))),
			pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "period_expires":
		w.reply(ctx, "请输入截止（"+w.dr.Values["precision"]+"，时区 "+w.user.Timezone+"）。\n"+expirationHelp, navRows(true, false)...)
	case "period_starts":
		w.reply(ctx, "可选：开始时间，使用带 offset 的 RFC3339，且必须早于截止。可跳过。\n示例：2027-01-01T00:00:00+08:00", navRows(true, true)...)
	case "period_preview":
		w.reply(ctx, periodAddPreviewText(item, w.dr), pair(button("确认创建", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "添加有效期流程状态无效")
	}
}

func periodAddPreviewText(item service.ServiceItem, d draft) string {
	name := shown(d, "name")
	return fmt.Sprintf("请确认新建有效期：\n服务：%s\n类型：%s\n名称：%s\n精度：%s\n截止：%s\n开始：%s\n\n创建后不会自动创建提醒规则；成功后可从结果页添加规则。",
		item.Name, d.Values["period_type"], name, d.Values["precision"], d.Values["expires_at"], shown(d, "starts_at"))
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
		if err := parsePrecisionValue(w.dr.Values["precision"], value); err != nil {
			w.stepError(ctx, err.Error(), true, false)
			return
		}
		w.dr.set("expires_at", value)
	case "period_starts":
		if value != "" {
			at, err := time.Parse(time.RFC3339, value)
			if err != nil {
				w.stepError(ctx, "开始时间必须是带 offset 的 RFC3339，例如 2027-01-01T00:00:00+08:00。", true, true)
				return
			}
			expires, err := time.Parse(time.RFC3339, w.dr.Values["expires_at"])
			if w.dr.Values["precision"] == "date" {
				loc, locErr := time.LoadLocation(w.user.Timezone)
				if locErr != nil {
					w.stepError(ctx, "当前时区无效，请先修改时区。", true, true)
					return
				}
				expires, err = time.ParseInLocation("2006-01-02", w.dr.Values["expires_at"], loc)
			}
			if err != nil || !at.Before(expires) {
				w.stepError(ctx, "开始时间必须早于截止时间。", true, true)
				return
			}
		}
		w.dr.set("starts_at", value)
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

func (w *wizard) confirmPeriodAdd(ctx context.Context) {
	in := service.PeriodInput{PeriodType: w.dr.Values["period_type"], Name: w.dr.Values["name"],
		DatePrecision: w.dr.Values["precision"], ExpiresAt: w.dr.Values["expires_at"]}
	if starts := strings.TrimSpace(w.dr.Values["starts_at"]); starts != "" {
		in.StartsAt = &starts
	}
	p, err := w.h.svc.CreatePeriod(ctx, w.user.ID, *w.d.ServiceID, in)
	if err != nil {
		w.h.send(ctx, w.b, w.chatID, "创建有效期失败："+serviceMessage(err)+"\n草稿仍保留，可返回修改或取消。")
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, periodDetail(p)+"\n\n未自动创建提醒规则。", markup(
		pair(button("添加提醒规则", cb("rule", "add", id(p.ID))), button("查看有效期", cb("period", "view", id(p.ID)))),
		pair(button("查看服务", cb("service", "view", id(p.ServiceID)))),
	))
}

// ---- terms edit -----------------------------------------------------------

func (h *Handler) startTermsEdit(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑条款："+serviceMessage(err))
		return
	}
	if p.Status == "cancelled" {
		h.send(ctx, b, chatID, "有效期已取消，不能编辑条款。")
		return
	}
	if parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID); err != nil || parent.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请恢复服务后再编辑条款。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogTermsEdit, periodID, "terms_edit:"+id(periodID)) {
		return
	}
	cycle, revision := p.Cycle, p.Terms.Revision
	d := service.Dialog{ServiceID: &p.ServiceID, ValidityPeriodID: &periodID,
		ExpectedCycle: &cycle, ExpectedTermsRevision: &revision, Step: "terms_mode", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogTermsEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始条款编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
	w.dr.set("mode", p.Terms.Mode)
	w.persist(ctx, "terms_mode")
	w.renderTerms(ctx)
}

func (w *wizard) renderTerms(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.ValidityPeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	if p.Status == "cancelled" {
		w.abort(ctx, "有效期已取消")
		return
	}
	// Terms and cycle versions are re-read on every render so a conflict is
	// surfaced before the user commits, not silently applied to old state.
	if w.d.Step == "terms_preview" {
		if w.d.ExpectedCycle == nil || *w.d.ExpectedCycle != p.Cycle || w.d.ExpectedTermsRevision == nil || *w.d.ExpectedTermsRevision != p.Terms.Revision {
			w.dr.Resume, w.dr.Pending = "", ""
			w.reply(ctx, "条款或周期已被其他操作更新，本次未写入，草案已保留。\n最新条款："+termsText(p.Terms)+"\n\n可返回修改后重新确认，或发送 /cancel 取消。", navRows(true, false)...)
			return
		}
	}
	head := "有效期 #" + id(p.ID) + " " + periodName(p) + "\n当前条款：" + termsText(p.Terms) + "\n"
	switch w.d.Step {
	case "terms_mode":
		w.reply(ctx, head+"\n请选择条款模式：\nonce 一次性；recurring 周期重复；custom 自定义/无固定周期。\n修改条款不会改变周期、不会取消提醒任务。",
			pair(button("once", choiceCallback("mode", "once")), button("recurring", choiceCallback("mode", "recurring")), button("custom", choiceCallback("mode", "custom"))),
			pair(button("取消", cb("dialog", "cancel"))))
	case "terms_interval":
		w.reply(ctx, "请输入 recurring 周期，例如：\n1 month\n2 week\n单位可为 day/week/month/year。", navRows(true, false)...)
	case "terms_amount":
		w.reply(ctx, "约定费用（可选）：发送“金额 币种”，例如 2999 USD；金额为最小货币单位整数，币种为三位大写字母。\n约定费用不代表已付款。也可跳过。", navRows(true, true)...)
	case "terms_preview":
		w.reply(ctx, termsPreviewText(p, w.dr, w.d), pair(button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "条款流程状态无效")
	}
}

func termsPreviewText(p service.Period, d draft, dialog service.Dialog) string {
	t := service.PeriodTerms{Mode: d.Values["mode"]}
	if t.Mode == "recurring" {
		fields := strings.Fields(d.Values["interval"])
		if len(fields) == 2 {
			if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
				t.IntervalCount = &n
			}
			unit := fields[1]
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
	return fmt.Sprintf("请确认条款（有效期 #%s %s）：\n旧条款：%s\n新条款：%s\n\n保存时使用服务端保存的 cycle=%d、terms revision=%d；若已被其他操作更新会拒绝并保留草稿。\n约定费用不代表已付款。",
		id(p.ID), periodName(p), termsText(p.Terms), termsText(t), expected(dialog.ExpectedCycle), expected(dialog.ExpectedTermsRevision))
}

func expected(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func (w *wizard) textTerms(ctx context.Context, text string) {
	value := trimmed(text)
	switch w.d.Step {
	case "terms_interval":
		fields := strings.Fields(value)
		if len(fields) != 2 {
			w.stepError(ctx, "请发送“数量 单位”，例如 1 month。", true, false)
			return
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		switch fields[1] {
		case "day", "week", "month", "year":
		default:
			w.stepError(ctx, "单位必须是 day、week、month 或 year。", true, false)
			return
		}
		if err != nil || n < 1 {
			w.stepError(ctx, "数量必须是正整数。", true, false)
			return
		}
		w.dr.set("interval", fields[0]+" "+fields[1])
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
	next := nextStep(service.DialogTermsEdit, w.dr, w.d.Step)
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func (w *wizard) confirmTerms(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.ValidityPeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	if w.d.ExpectedCycle == nil || w.d.ExpectedTermsRevision == nil {
		w.abort(ctx, "条款对话缺少版本信息")
		return
	}
	if p.Cycle != *w.d.ExpectedCycle || p.Terms.Revision != *w.d.ExpectedTermsRevision {
		w.h.send(ctx, w.b, w.chatID, "条款或周期已被其他操作更新，本次未写入，草案仍保留。\n最新条款："+termsText(p.Terms)+"\n\n可返回修改后重新确认，或发送 /cancel 取消。")
		return
	}
	in := service.TermsInput{Mode: w.dr.Values["mode"]}
	if in.Mode == "recurring" {
		fields := strings.Fields(w.dr.Values["interval"])
		if len(fields) != 2 {
			w.stepError(ctx, "周期信息缺失，请返回修改。", false, false)
			return
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			w.stepError(ctx, "周期数量无效，请返回修改。", false, false)
			return
		}
		unit := fields[1]
		in.IntervalCount, in.IntervalUnit = &n, &unit
	}
	if amount := strings.TrimSpace(w.dr.Values["amount"]); amount != "" {
		n, err := strconv.ParseInt(amount, 10, 64)
		if err != nil {
			w.stepError(ctx, "金额无效，请返回修改。", false, false)
			return
		}
		currency := strings.ToUpper(strings.TrimSpace(w.dr.Values["currency"]))
		in.Amount, in.Currency = &n, &currency
	}
	patch := service.PeriodPatch{ExpectedCycle: *w.d.ExpectedCycle, ExpectedTermsRevision: w.d.ExpectedTermsRevision,
		Terms: service.Optional[service.TermsInput]{Set: true, Value: &in}}
	updated, err := w.h.svc.UpdatePeriod(ctx, w.user.ID, p.ID, patch)
	if err != nil {
		hint := "保存失败：" + serviceMessage(err) + "\n草稿仍保留，可返回修改或取消。"
		if service.Code(err) == "conflict" {
			hint = "保存失败：条款或周期已被其他操作更新，未写入。\n草稿仍保留；可取消后重新查看最新条款。最新条款：" + termsText(p.Terms)
		}
		w.h.send(ctx, w.b, w.chatID, hint)
		return
	}
	if _, err := w.h.svc.CancelDialog(ctx, w.user.ID); err != nil {
		w.h.logger.Warn("dialog cleanup failed", "code", "dialog_error")
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, periodDetail(updated), markup(
		pair(button("编辑条款", cb("period", "terms", id(updated.ID))), button("查看有效期", cb("period", "view", id(updated.ID)))),
	))
}

// ---- reminder rules -------------------------------------------------------

func (h *Handler) startRuleAdd(ctx context.Context, b *telegram.Bot, user service.User, chatID, periodID int64) {
	p, err := h.svc.GetPeriod(ctx, user.ID, periodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法添加提醒规则："+serviceMessage(err))
		return
	}
	if p.Status == "cancelled" {
		h.send(ctx, b, chatID, "有效期已取消，不能添加提醒规则。")
		return
	}
	if parent, err := h.svc.GetService(ctx, user.ID, p.ServiceID); err != nil || parent.Status != "active" {
		h.send(ctx, b, chatID, "服务已归档，请恢复服务后再添加提醒规则。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRuleAdd, periodID, "rule_add:"+id(periodID)) {
		return
	}
	d := service.Dialog{ServiceID: &p.ServiceID, ValidityPeriodID: &periodID, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogRuleAdd, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始提醒规则流程："+serviceMessage(err))
		return
	}
	{
		w := h.wizard(ctx, b, user, start)
		w.renderRule(ctx)
	}
}

func (h *Handler) startRuleEdit(ctx context.Context, b *telegram.Bot, user service.User, chatID, ruleID int64) {
	r, err := h.svc.GetRule(ctx, user.ID, ruleID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑规则："+serviceMessage(err))
		return
	}
	period, err := h.svc.GetPeriod(ctx, user.ID, r.ValidityPeriodID)
	if err != nil {
		h.send(ctx, b, chatID, "无法编辑规则："+serviceMessage(err))
		return
	}
	parent, err := h.svc.GetService(ctx, user.ID, period.ServiceID)
	if err != nil || period.Status == "cancelled" || parent.Status != "active" {
		h.send(ctx, b, chatID, "有效期或服务不可编辑，请先恢复服务。")
		return
	}
	if h.conflictTarget(ctx, b, user, chatID, service.DialogRuleEdit, ruleID, "rule_edit:"+id(ruleID)) {
		return
	}
	revision := r.Revision
	d := service.Dialog{ValidityPeriodID: &r.ValidityPeriodID, RuleID: &ruleID, ExpectedRevision: &revision, Step: "rule_days", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogRuleEdit, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始规则编辑："+serviceMessage(err))
		return
	}
	w := h.wizard(ctx, b, user, start)
	w.dr.set("days_before", strconv.Itoa(r.DaysBefore))
	w.dr.set("reminder_time", r.ReminderTime)
	w.dr.set("enabled", strconv.FormatBool(r.Enabled))
	w.persist(ctx, "rule_days")
	w.renderRule(ctx)
}

func (w *wizard) renderRule(ctx context.Context) {
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.ValidityPeriodID)
	if err != nil {
		w.abort(ctx, "有效期已不存在")
		return
	}
	if p.Status == "cancelled" {
		w.abort(ctx, "有效期已取消")
		return
	}
	head := "有效期 #" + id(p.ID) + " " + periodName(p) + "\n截止：" + periodExpiry(p) + "\n"
	if w.d.Action == service.DialogRuleEdit && w.d.RuleID != nil {
		if r, err := w.h.svc.GetRule(ctx, w.user.ID, *w.d.RuleID); err == nil {
			head += fmt.Sprintf("规则 #%s 当前：提前 %d 天 %s，启用 %t\n", id(r.ID), r.DaysBefore, r.ReminderTime, r.Enabled)
		}
	}
	switch w.d.Step {
	case "rule_days":
		w.reply(ctx, head+"\n请输入提前天数（大于等于 0 的整数），例如 7。", navRows(!w.isRuleEditFirst(), false)...)
	case "rule_time":
		w.reply(ctx, "请输入提醒时间 HH:MM（24 小时制，用户时区 "+w.user.Timezone+"），例如 09:00。", navRows(true, false)...)
	case "rule_enabled":
		w.reply(ctx, "是否启用该规则？", pair(button("启用", choiceCallback("enabled", "true")), button("禁用", choiceCallback("enabled", "false"))),
			pair(button("上一步", cb("dialog", "back")), button("取消", cb("dialog", "cancel"))))
	case "rule_preview":
		w.reply(ctx, rulePreviewText(p, w.dr), pair(button("确认保存", cb("dialog", "confirm")), button("返回修改", cb("dialog", "back"))), pair(button("取消", cb("dialog", "cancel"))))
	default:
		w.abort(ctx, "提醒规则流程状态无效")
	}
}

func (w *wizard) isRuleEditFirst() bool { return w.d.Action == service.DialogRuleEdit }

func rulePreviewText(p service.Period, d draft) string {
	verb := "新建提醒规则"
	if d.Values["enabled"] == "false" {
		verb = "新建提醒规则（禁用状态）"
	}
	return fmt.Sprintf("请确认%s：\n有效期：#%s %s\n截止：%s\n提前：%s 天\n提醒时间：%s\n启用：%s\n\n保存时由服务层重新校验提醒时间是否早于截止。",
		verb, id(p.ID), periodName(p), periodExpiry(p), d.Values["days_before"], d.Values["reminder_time"], d.Values["enabled"])
}

func (w *wizard) textRule(ctx context.Context, text string) {
	value := trimmed(text)
	switch w.d.Step {
	case "rule_days":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 99999 {
			w.stepError(ctx, "提前天数必须是 0 到 99999 之间的整数。", !w.isRuleEditFirst(), false)
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
	p, err := w.h.svc.GetPeriod(ctx, w.user.ID, *w.d.ValidityPeriodID)
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
	rows := [][]models.InlineKeyboardButton{
		pair(button("编辑规则", cb("rule", "edit", id(rule.ID))), button("禁用规则", cb("rule", "disable", id(rule.ID)))),
		pair(button("查看有效期", cb("period", "view", id(rule.ValidityPeriodID)))),
	}
	w.h.sendMarkup(ctx, w.b, w.chatID, label+"\n"+ruleLinePlain(rule), markup(rows...))
}

// ---- timezone -------------------------------------------------------------

func (h *Handler) startTimezone(ctx context.Context, b *telegram.Bot, user service.User, chatID int64) {
	if h.conflictTarget(ctx, b, user, chatID, service.DialogTimezone, 0, "timezone") {
		return
	}
	d := service.Dialog{Step: "tz_value", Draft: "{}"}
	start, err := h.svc.StartDialog(ctx, user.ID, chatID, service.DialogTimezone, d.Step, d)
	if err != nil {
		h.send(ctx, b, chatID, "无法开始修改时区："+serviceMessage(err))
		return
	}
	{
		w := h.wizard(ctx, b, user, start)
		w.renderTimezone(ctx)
	}
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
		w.reply(ctx, fmt.Sprintf("请确认修改时区：\n旧时区：%s\n新时区：%s\n\ndate 精度的截止日期会按新时区重算当地日历日结束；datetime 的绝对时刻保持不变。修改时区会推进相关有效期版本并取消未完成提醒任务。",
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
	w.h.sendMarkup(ctx, w.b, w.chatID, "时区已更新为 "+user.Timezone+"。\ndate 精度的截止日期已按新时区重算，datetime 的绝对时刻不变。",
		markup(pair(button("返回设置", cb("settings", "view")))))
}
