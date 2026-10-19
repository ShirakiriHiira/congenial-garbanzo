package bot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"telegram.bot.remind/internal/service"
)

const serviceUsage = "用法：/service add <query> | list [after_id] | view <id> | edit <id> <query> | archive <id> | categories [after_id] | category name=名称 | category-edit <id> name=名称 | period new <service_id> <query> | terms <period_id> <query>"

var serviceFields = []string{"name", "category_id", "provider", "identifier", "management_url", "notes", "card_type", "phone_number", "country_or_region", "iccid"}
var serviceEditFields = []string{"name", "category_id", "provider", "identifier", "management_url", "notes", "card_type", "phone_number", "country_or_region", "iccid", "status"}

func (h *Handler) commandService(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) == 0 {
		return "", fail(serviceUsage)
	}
	switch args[0] {
	case "add":
		v, err := query(strings.Join(args[1:], " "), serviceFields...)
		if err != nil {
			return "", err
		}
		categoryID, err := requiredInt(v, "category_id", 1)
		if err != nil {
			return "", err
		}
		in := service.ServiceInput{Name: strings.TrimSpace(v.Get("name")), CategoryID: categoryID,
			Provider: optionalValue(v.Get("provider")), Identifier: optionalValue(v.Get("identifier")),
			ManagementURL: optionalValue(v.Get("management_url")), Notes: optionalValue(v.Get("notes"))}
		if hasSIMFields(v) {
			in.SIMDetails = &service.SIMDetails{CardType: strings.TrimSpace(v.Get("card_type")), PhoneNumber: optionalValue(v.Get("phone_number")), CountryOrRegion: optionalValue(v.Get("country_or_region")), ICCID: optionalValue(v.Get("iccid"))}
		}
		item, err := h.svc.CreateService(ctx, user.ID, in)
		if err != nil {
			return "", err
		}
		return h.serviceDetail(ctx, user.ID, item)
	case "list":
		after, err := optionalID(strings.Join(args[1:], " "))
		if err != nil {
			return "", err
		}
		page, err := h.svc.ListServices(ctx, user.ID, after, pageSize, service.ServiceFilter{Status: "active"})
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("服务列表（仅活跃）：\n")
		if len(page.Data) == 0 {
			b.WriteString("（无）使用 /service categories 查看类别，再用 /service add name=名称&category_id=类别ID 添加。")
		}
		for _, item := range page.Data {
			fmt.Fprintf(&b, "#%s %s [%s]\n  服务商：%s\n  详情：/service view %s\n", id(item.ID), item.Name, item.Status, textOrDash(item.Provider), id(item.ID))
		}
		appendNext(&b, page.NextCursor, "/service list")
		return b.String(), nil
	case "view", "archive":
		if len(args) != 2 {
			return "", fail(serviceUsage)
		}
		objectID, err := parseID(args[1])
		if err != nil {
			return "", err
		}
		if args[0] == "archive" {
			if err := h.svc.ArchiveService(ctx, user.ID, objectID); err != nil {
				return "", err
			}
			return "服务已归档。恢复：/service edit " + id(objectID) + " status=active", nil
		}
		item, err := h.svc.GetService(ctx, user.ID, objectID)
		if err != nil {
			return "", err
		}
		return h.serviceDetail(ctx, user.ID, item)
	case "edit":
		if len(args) < 3 {
			return "", fail(serviceUsage)
		}
		objectID, err := parseID(args[1])
		if err != nil {
			return "", err
		}
		v, err := query(strings.Join(args[2:], " "), serviceEditFields...)
		if err != nil {
			return "", err
		}
		var in service.ServicePatch
		if s, ok := v["name"]; ok {
			name := strings.TrimSpace(s[0])
			in.Name = &name
		}
		if _, ok := v["category_id"]; ok {
			n, err := requiredInt(v, "category_id", 1)
			if err != nil {
				return "", err
			}
			in.CategoryID = &n
		}
		in.Provider = queryOptional(v, "provider")
		in.Identifier = queryOptional(v, "identifier")
		in.ManagementURL = queryOptional(v, "management_url")
		in.Notes = queryOptional(v, "notes")
		if s, ok := v["status"]; ok {
			x := strings.TrimSpace(s[0])
			in.Status = &x
		}
		if hasSIMFields(v) {
			details := service.SIMDetailsPatch{CardType: queryOptional(v, "card_type"), PhoneNumber: queryOptional(v, "phone_number"), CountryOrRegion: queryOptional(v, "country_or_region"), ICCID: queryOptional(v, "iccid")}
			in.SIMDetails = service.Optional[service.SIMDetailsPatch]{Set: true, Value: &details}
		}
		item, err := h.svc.UpdateService(ctx, user.ID, objectID, in)
		if err != nil {
			return "", err
		}
		return h.serviceDetail(ctx, user.ID, item)
	case "categories":
		after, err := optionalID(strings.Join(args[1:], " "))
		if err != nil {
			return "", err
		}
		page, err := h.svc.ListCategories(ctx, user.ID, after, pageSize)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("服务类别：\n")
		for _, c := range page.Data {
			fmt.Fprintf(&b, "#%s %s [%s", id(c.ID), c.Name, c.Kind)
			if c.UserID == nil {
				b.WriteString(" / 内置只读")
			} else {
				b.WriteString(" / 自定义")
			}
			b.WriteString("]\n")
		}
		appendNext(&b, page.NextCursor, "/service categories")
		return b.String(), nil
	case "category", "category-edit":
		start := 1
		var categoryID int64
		if args[0] == "category-edit" {
			if len(args) < 3 {
				return "", fail(serviceUsage)
			}
			var err error
			categoryID, err = parseID(args[1])
			if err != nil {
				return "", err
			}
			start = 2
		}
		v, err := query(strings.Join(args[start:], " "), "name")
		if err != nil {
			return "", err
		}
		var c service.Category
		if args[0] == "category" {
			c, err = h.svc.CreateCategory(ctx, user.ID, v.Get("name"))
		} else {
			c, err = h.svc.UpdateCategory(ctx, user.ID, categoryID, v.Get("name"))
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("类别 #%s：%s [%s]", id(c.ID), c.Name, c.Kind), nil
	case "period":
		if len(args) < 4 || args[1] != "new" {
			return "", fail("用法：/service period new <service_id> period_type=custom&name=名称&date_precision=date&expires_at=2027-01-31")
		}
		serviceID, err := parseID(args[2])
		if err != nil {
			return "", err
		}
		v, err := query(strings.Join(args[3:], " "), "period_type", "name", "date_precision", "expires_at", "starts_at")
		if err != nil {
			return "", err
		}
		p, err := h.svc.CreatePeriod(ctx, user.ID, serviceID, service.PeriodInput{
			PeriodType: strings.TrimSpace(v.Get("period_type")), Name: strings.TrimSpace(v.Get("name")),
			DatePrecision: strings.TrimSpace(v.Get("date_precision")), ExpiresAt: strings.TrimSpace(v.Get("expires_at")), StartsAt: optionalValue(v.Get("starts_at")),
		})
		if err != nil {
			return "", err
		}
		return periodDetail(p) + fmt.Sprintf("\n\n添加提醒：/edit rule new %s days_before=7&reminder_time=09:00", id(p.ID)), nil
	case "terms":
		if len(args) < 3 {
			return "", fail("用法：/service terms <period_id> expected_cycle=1&expected_terms_revision=1&mode=custom|once|recurring[&interval_count=1&interval_unit=month][&amount=100&currency=USD]")
		}
		periodID, err := parseID(args[1])
		if err != nil {
			return "", err
		}
		patch, err := parseTermsPatch(strings.Join(args[2:], " "))
		if err != nil {
			return "", err
		}
		p, err := h.svc.UpdatePeriod(ctx, user.ID, periodID, patch)
		if err != nil {
			return "", err
		}
		return periodDetail(p), nil
	default:
		return "", fail(serviceUsage)
	}
}

func queryOptional(v url.Values, key string) service.Optional[string] {
	s, ok := v[key]
	if !ok {
		return service.Optional[string]{}
	}
	return service.Optional[string]{Set: true, Value: optionalValue(s[0])}
}

func hasSIMFields(v url.Values) bool {
	for _, key := range []string{"card_type", "phone_number", "country_or_region", "iccid"} {
		if _, ok := v[key]; ok {
			return true
		}
	}
	return false
}

func parseTermsPatch(raw string) (service.PeriodPatch, error) {
	v, err := query(raw, "expected_cycle", "expected_terms_revision", "mode", "interval_count", "interval_unit", "amount", "currency")
	if err != nil {
		return service.PeriodPatch{}, err
	}
	cycle, err := requiredInt(v, "expected_cycle", 1)
	if err != nil {
		return service.PeriodPatch{}, err
	}
	revision, err := requiredInt(v, "expected_terms_revision", 1)
	if err != nil {
		return service.PeriodPatch{}, err
	}
	in := service.TermsInput{Mode: strings.TrimSpace(v.Get("mode"))}
	if in.Mode != "once" && in.Mode != "recurring" && in.Mode != "custom" {
		return service.PeriodPatch{}, fail("mode 必须是 once、recurring 或 custom。")
	}
	if in.Mode == "recurring" {
		count, err := requiredInt(v, "interval_count", 1)
		if err != nil {
			return service.PeriodPatch{}, err
		}
		unit := strings.TrimSpace(v.Get("interval_unit"))
		if unit != "day" && unit != "week" && unit != "month" && unit != "year" {
			return service.PeriodPatch{}, fail("interval_unit 必须是 day、week、month 或 year。")
		}
		in.IntervalCount, in.IntervalUnit = &count, &unit
	}
	_, amountSet := v["amount"]
	_, currencySet := v["currency"]
	if amountSet != currencySet {
		return service.PeriodPatch{}, fail("amount 与 currency 必须同时提供。")
	}
	if amountSet {
		amount, err := strconv.ParseInt(strings.TrimSpace(v.Get("amount")), 10, 64)
		if err != nil || amount < 0 {
			return service.PeriodPatch{}, fail("amount 必须是大于等于 0 的整数（最小货币单位）。")
		}
		currency := strings.ToUpper(strings.TrimSpace(v.Get("currency")))
		if len(currency) != 3 || currency[0] < 'A' || currency[0] > 'Z' || currency[1] < 'A' || currency[1] > 'Z' || currency[2] < 'A' || currency[2] > 'Z' {
			return service.PeriodPatch{}, fail("currency 必须是三个英文字母。")
		}
		in.Amount, in.Currency = &amount, &currency
	}
	return service.PeriodPatch{ExpectedCycle: cycle, ExpectedTermsRevision: &revision, Terms: service.Optional[service.TermsInput]{Set: true, Value: &in}}, nil
}

func termsText(t service.PeriodTerms) string {
	var b strings.Builder
	fmt.Fprintf(&b, "约定条款：%s（版本 %d）", t.Mode, t.Revision)
	if t.IntervalCount != nil && t.IntervalUnit != nil {
		fmt.Fprintf(&b, "，每 %d %s", *t.IntervalCount, *t.IntervalUnit)
	}
	if t.Amount != nil && t.Currency != nil {
		fmt.Fprintf(&b, "，约定费用 %d %s（最小货币单位，非已付款）", *t.Amount, *t.Currency)
	}
	b.WriteString("；仅记录，不自动扣款或推进截止。")
	return b.String()
}

// serviceDetail keeps the advanced text-only command output bounded: it
// renders the first period page and points at the interactive view for more.
func (h *Handler) serviceDetail(ctx context.Context, uid int64, item service.ServiceItem) (string, error) {
	text, page, err := h.serviceDetailPage(ctx, uid, item, 0)
	if err != nil {
		return "", err
	}
	if page.NextCursor != nil {
		text += "\n\n更多有效期：/service view " + id(item.ID)
	}
	return text, nil
}
