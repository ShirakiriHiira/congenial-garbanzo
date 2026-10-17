package bot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"
)

const pageSize = 10

func (h *Handler) commandAdd(ctx context.Context, user service.User, raw string) (string, error) {
	v, err := query(raw, "name", "card_type", "operator", "phone_number", "country_or_region", "iccid", "notes")
	if err != nil {
		return "", err
	}
	name, cardType := strings.TrimSpace(v.Get("name")), strings.TrimSpace(v.Get("card_type"))
	if name == "" || cardType == "" {
		return "", fail("用法：/add name=日本卡&card_type=esim&operator=...（name 与 card_type 必填）")
	}
	sim, err := h.svc.CreateSIM(ctx, user.ID, service.SIMInput{
		Name:            name,
		CardType:        cardType,
		Operator:        optionalValue(v.Get("operator")),
		PhoneNumber:     optionalValue(v.Get("phone_number")),
		CountryOrRegion: optionalValue(v.Get("country_or_region")),
		ICCID:           optionalValue(v.Get("iccid")),
		Notes:           optionalValue(v.Get("notes")),
	})
	if err != nil {
		return "", err
	}
	return simDetail(sim) + "\n\n添加有效期：/edit period new " + id(sim.ID) + " period_type=number_validity&name=名称&date_precision=date&expires_at=2027-01-31", nil
}

func (h *Handler) commandList(ctx context.Context, user service.User, raw string) (string, error) {
	after, err := optionalID(raw)
	if err != nil {
		return "", err
	}
	page, err := h.svc.ListSIMs(ctx, user.ID, after, pageSize)
	if err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "还没有卡片。使用 /add name=...&card_type=esim 添加。", nil
	}
	var b strings.Builder
	b.WriteString("卡片列表：\n")
	for _, sim := range page.Data {
		fmt.Fprintf(&b, "#%s %s  [%s / %s]\n", id(sim.ID), sim.Name, sim.CardType, sim.Status)
		fmt.Fprintf(&b, "  运营商：%s\n", textOrDash(sim.Operator))
		if p := h.latestPeriod(ctx, user.ID, sim.ID); p != nil {
			fmt.Fprintf(&b, "  最近有效期：#%s %s %s\n", id(p.ID), periodExpiry(*p), p.Status)
		} else {
			b.WriteString("  最近有效期：未配置\n")
		}
	}

	appendNext(&b, page.NextCursor, "/list")
	return b.String(), nil
}

// latestPeriod is a display convenience only; authorization is enforced by the
// service call. Errors are intentionally collapsed to "no latest period".
func (h *Handler) latestPeriod(ctx context.Context, uid, simID int64) *service.Period {
	page, err := h.svc.ListPeriods(ctx, uid, simID, 0, 100)
	if err != nil || len(page.Data) == 0 {
		return nil
	}
	best := page.Data[0]
	for _, p := range page.Data[1:] {
		if p.ExpiresAt > best.ExpiresAt {
			best = p
		}
	}
	return &best
}

func (h *Handler) commandView(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) < 2 {
		return "", fail("用法：/view sim <id> | /view period <id> | /view sim <id> periods [after_id] | /view period <id> rules|renewals [after_id]")
	}
	kind := args[0]
	objectID, err := parseID(args[1])
	if err != nil {
		return "", fail("用法：/view sim <id> | /view period <id>")
	}
	switch kind {
	case "sim":
		sim, err := h.svc.GetSIM(ctx, user.ID, objectID)
		if err != nil {
			return "", err
		}
		if len(args) == 2 {
			return simDetail(sim) + "\n\n有效期列表：/view sim " + id(sim.ID) + " periods", nil
		}
		if len(args) > 4 || args[2] != "periods" {
			return "", fail("用法：/view sim <id> periods [after_id]")
		}
		after := int64(0)
		if len(args) == 4 {
			if after, err = parseAfter(args[3]); err != nil {
				return "", fail("after_id 必须是非负整数。")
			}
		}
		page, err := h.svc.ListPeriods(ctx, user.ID, sim.ID, after, pageSize)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "卡片 #%s %s 的有效期：\n", id(sim.ID), sim.Name)
		if len(page.Data) == 0 {
			b.WriteString("（无）")
		}
		for _, p := range page.Data {
			fmt.Fprintf(&b, "#%s %s  %s  状态 %s  版本 %d\n", id(p.ID), p.Name, periodExpiry(p), p.Status, p.Cycle)
			fmt.Fprintf(&b, "  类型：%s  提醒：/view period %s rules\n", p.PeriodType, id(p.ID))
		}
		appendNext(&b, page.NextCursor, "/view sim "+id(sim.ID)+" periods")
		return b.String(), nil
	case "period":
		p, err := h.svc.GetPeriod(ctx, user.ID, objectID)
		if err != nil {
			return "", err
		}
		if len(args) == 2 {
			rules, err := h.svc.ListRules(ctx, user.ID, p.ID, 0, 100)
			if err != nil {
				return "", err
			}
			status := "未配置提醒（不会发送通知）"
			if len(rules.Data) > 0 {
				status = fmt.Sprintf("%d 条规则", len(rules.Data))
			}
			return periodDetail(p) + fmt.Sprintf("\n提醒：%s\n\n提醒规则：/view period %s rules\n续期历史：/view period %s renewals", status, id(p.ID), id(p.ID)), nil
		}
		if len(args) > 4 {
			return "", fail("用法：/view period <id> rules|renewals [after_id]")
		}
		section := args[2]
		if section != "rules" && section != "renewals" {
			return "", fail("用法：/view period <id> rules|renewals [after_id]")
		}
		after := int64(0)
		if len(args) == 4 {
			if after, err = parseAfter(args[3]); err != nil {
				return "", fail("after_id 必须是非负整数。")
			}
		}
		if section == "rules" {
			page, err := h.svc.ListRules(ctx, user.ID, p.ID, after, pageSize)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "有效期 #%s %s 的提醒规则：\n", id(p.ID), p.Name)
			if len(page.Data) == 0 {
				b.WriteString("（未配置提醒；如需通知请添加规则）")
			}
			for _, r := range page.Data {
				fmt.Fprintf(&b, "#%s 提前 %d 天 %s  启用 %t  版本 %d\n", id(r.ID), r.DaysBefore, r.ReminderTime, r.Enabled, r.Revision)
			}
			appendNext(&b, page.NextCursor, "/view period "+id(p.ID)+" rules")
			return b.String(), nil
		}
		page, err := h.svc.ListRenewals(ctx, user.ID, p.ID, after, pageSize)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "有效期 #%s %s 的续期历史：\n", id(p.ID), p.Name)
		if len(page.Data) == 0 {
			b.WriteString("（无）")
		}
		for _, r := range page.Data {
			fmt.Fprintf(&b, "#%s 周期 %d：%s → %s", id(r.ID), r.PreviousCycle, stamp(r.PreviousExpiresAt, r.Timezone), stamp(r.NewExpiresAt, r.Timezone))
			if r.Amount != nil && r.Currency != nil {
				fmt.Fprintf(&b, "  金额 %d %s", *r.Amount, *r.Currency)
			}
			b.WriteString("\n")
			if r.Notes != nil && *r.Notes != "" {
				fmt.Fprintf(&b, "  备注：%s\n", *r.Notes)
			}
		}
		appendNext(&b, page.NextCursor, "/view period "+id(p.ID)+" renewals")
		return b.String(), nil
	default:
		return "", fail("用法：/view sim <id> | /view period <id>")
	}
}

func (h *Handler) commandEdit(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) < 2 {
		return "", fail("用法：/edit sim <id> <query> | /edit period new <sim_id> <query> | /edit period <id> <query> | /edit rule new <period_id> <query> | /edit rule <id> <query> | /edit timezone <IANA>")
	}
	switch args[0] {
	case "timezone":
		if len(args) != 2 {
			return "", fail("用法：/edit timezone Asia/Taipei")
		}
		if _, err := time.LoadLocation(args[1]); err != nil {
			return "", fail("时区必须是合法 IANA 名称，例如 Asia/Taipei。")
		}
		if _, err := h.svc.SetTimezone(ctx, user.ID, args[1]); err != nil {
			return "", err
		}
		return "时区已更新为 " + args[1] + "。date 精度的截止日期按新时区重算，datetime 的绝对时刻保持不变。", nil
	case "sim":
		if len(args) < 3 {
			return "", fail("用法：/edit sim <id> <query>，例如 /edit sim 1 name=新名称")
		}
		objectID, err := parseID(args[1])
		if err != nil {
			return "", fail("卡片 ID 必须是正整数。")
		}
		v, err := query(strings.Join(args[2:], " "), "name", "card_type", "operator", "phone_number", "country_or_region", "iccid", "notes", "status")
		if err != nil {
			return "", err
		}
		if len(v) == 0 {
			return "", fail("未提供要修改的字段。")
		}
		patch := service.SIMPatch{}
		if s, ok := v["name"]; ok {
			x := strings.TrimSpace(s[0])
			if x == "" {
				return "", fail("name 不能为空。")
			}
			patch.Name = &x
		}
		if s, ok := v["card_type"]; ok {
			patch.CardType = &s[0]
		}
		if s, ok := v["status"]; ok {
			patch.Status = &s[0]
		}
		for key, dest := range map[string]*service.Optional[string]{
			"operator": &patch.Operator, "phone_number": &patch.PhoneNumber,
			"country_or_region": &patch.CountryOrRegion, "iccid": &patch.ICCID, "notes": &patch.Notes,
		} {
			if s, ok := v[key]; ok {
				*dest = service.Optional[string]{Set: true, Value: optionalValue(s[0])}
			}
		}
		sim, err := h.svc.UpdateSIM(ctx, user.ID, objectID, patch)
		if err != nil {
			return "", err
		}
		return simDetail(sim), nil
	case "period":
		if args[1] == "new" {
			if len(args) < 4 {
				return "", fail("用法：/edit period new <sim_id> period_type=number_validity&name=名称&date_precision=date&expires_at=2027-01-31")
			}
			simID, err := parseID(args[2])
			if err != nil {
				return "", fail("卡片 ID 必须是正整数。")
			}
			v, err := query(strings.Join(args[3:], " "), "period_type", "name", "date_precision", "expires_at", "starts_at")
			if err != nil {
				return "", err
			}
			in := service.PeriodInput{
				PeriodType:    strings.TrimSpace(v.Get("period_type")),
				Name:          strings.TrimSpace(v.Get("name")),
				DatePrecision: strings.TrimSpace(v.Get("date_precision")),
				ExpiresAt:     strings.TrimSpace(v.Get("expires_at")),
			}
			if in.PeriodType == "" || in.Name == "" || in.DatePrecision == "" || in.ExpiresAt == "" {
				return "", fail("用法：/edit period new <sim_id> period_type=number_validity&name=名称&date_precision=date&expires_at=2027-01-31")
			}
			if s := strings.TrimSpace(v.Get("starts_at")); s != "" {
				in.StartsAt = &s
			}
			p, err := h.svc.CreatePeriod(ctx, user.ID, simID, in)
			if err != nil {
				return "", err
			}
			return periodDetail(p) + fmt.Sprintf("\n\n添加提醒：/edit rule new %s days_before=7&reminder_time=09:00", id(p.ID)), nil
		}
		if len(args) < 3 {
			return "", fail("用法：/edit period <id> expected_cycle=1&name=新名称")
		}
		periodID, err := parseID(args[1])
		if err != nil {
			return "", fail("有效期 ID 必须是正整数。")
		}
		v, err := query(strings.Join(args[2:], " "), "expected_cycle", "period_type", "name", "date_precision", "expires_at", "starts_at")
		if err != nil {
			return "", err
		}
		cycle, err := requiredInt(v, "expected_cycle", 1)
		if err != nil {
			return "", err
		}
		patch := service.PeriodPatch{ExpectedCycle: cycle}
		if s, ok := v["period_type"]; ok {
			patch.PeriodType = &s[0]
		}
		if s, ok := v["name"]; ok {
			x := strings.TrimSpace(s[0])
			if x == "" {
				return "", fail("name 不能为空。")
			}
			patch.Name = &x
		}
		if s, ok := v["date_precision"]; ok {
			patch.DatePrecision = &s[0]
		}
		if s, ok := v["expires_at"]; ok {
			patch.ExpiresAt = &s[0]
		}
		if s, ok := v["starts_at"]; ok {
			patch.StartsAt = service.Optional[string]{Set: true, Value: optionalValue(s[0])}
		}
		updated, err := h.svc.UpdatePeriod(ctx, user.ID, periodID, patch)
		if err != nil {
			return "", err
		}
		return periodDetail(updated), nil
	case "rule":
		if args[1] == "new" {
			if len(args) < 4 {
				return "", fail("用法：/edit rule new <period_id> days_before=7&reminder_time=09:00")
			}
			periodID, err := parseID(args[2])
			if err != nil {
				return "", fail("有效期 ID 必须是正整数。")
			}
			v, err := query(strings.Join(args[3:], " "), "days_before", "reminder_time", "enabled")
			if err != nil {
				return "", err
			}
			days, err := requiredInt(v, "days_before", 0)
			if err != nil {
				return "", err
			}
			reminder := strings.TrimSpace(v.Get("reminder_time"))
			if reminder == "" {
				return "", fail("用法：/edit rule new <period_id> days_before=7&reminder_time=09:00")
			}
			in := service.RuleInput{DaysBefore: int(days), ReminderTime: reminder}
			if s, ok := v["enabled"]; ok {
				enabled, err := parseBool(s[0])
				if err != nil {
					return "", err
				}
				in.Enabled = &enabled
			}
			rule, err := h.svc.CreateRule(ctx, user.ID, periodID, in)
			if err != nil {
				return "", err
			}
			return ruleLine(rule), nil
		}
		if len(args) < 3 {
			return "", fail("用法：/edit rule <id> expected_revision=1&days_before=3&reminder_time=09:00")
		}
		ruleID, err := parseID(args[1])
		if err != nil {
			return "", fail("规则 ID 必须是正整数。")
		}
		v, err := query(strings.Join(args[2:], " "), "expected_revision", "days_before", "reminder_time", "enabled")
		if err != nil {
			return "", err
		}
		revision, err := requiredInt(v, "expected_revision", 1)
		if err != nil {
			return "", err
		}
		patch := service.RulePatch{ExpectedRevision: revision}
		if s, ok := v["days_before"]; ok {
			d, err := strconv.Atoi(strings.TrimSpace(s[0]))
			if err != nil || d < 0 {
				return "", fail("days_before 必须是大于等于 0 的整数。")
			}
			patch.DaysBefore = &d
		}
		if s, ok := v["reminder_time"]; ok {
			patch.ReminderTime = &s[0]
		}
		if s, ok := v["enabled"]; ok {
			enabled, err := parseBool(s[0])
			if err != nil {
				return "", err
			}
			patch.Enabled = &enabled
		}
		rule, err := h.svc.UpdateRule(ctx, user.ID, ruleID, patch)
		if err != nil {
			return "", err
		}
		return ruleLine(rule), nil
	default:
		return "", fail("用法：/edit sim|period|rule ... 或 /edit timezone <IANA>")
	}
}

func (h *Handler) commandDelete(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) == 2 && args[0] == "sim" {
		simID, err := parseID(args[1])
		if err != nil {
			return "", fail("卡片 ID 必须是正整数。")
		}
		if err := h.svc.ArchiveSIM(ctx, user.ID, simID); err != nil {
			return "", err
		}
		return "卡片已归档；已取消的提醒不会自动恢复。", nil
	}
	if len(args) == 3 && args[0] == "period" {
		periodID, err := parseID(args[1])
		if err != nil {
			return "", fail("有效期 ID 必须是正整数。")
		}
		cycle, err := parseID(args[2])
		if err != nil {
			return "", fail("expected_cycle 必须是正整数。")
		}
		if err := h.svc.CancelPeriod(ctx, user.ID, periodID, cycle); err != nil {
			return "", err
		}
		return "有效期已取消；历史续期和已发送提醒保留。", nil
	}
	return "", fail("用法：/delete sim <id> | /delete period <id> <expected_cycle>")
}

func (h *Handler) commandReminders(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) > 2 {
		return "", fail("用法：/reminders [pending|sending|sent|failed|cancelled|all] [after_id]")
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
			return "", fail("after_id 必须是非负整数。")
		}
	}
	page, err := h.svc.ListJobs(ctx, user.ID, filter, after, pageSize)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "提醒任务（%s）：\n", filter)
	if len(page.Data) == 0 {
		b.WriteString("（无）")
	}
	for _, j := range page.Data {
		fmt.Fprintf(&b, "#%s 项目 %s 周期 %d 状态 %s 尝试 %d\n", id(j.ID), id(j.ValidityPeriodID), j.Cycle, j.Status, j.Attempts)
		fmt.Fprintf(&b, "  计划 %s", stamp(j.ScheduledAt, user.Timezone))
		if j.SentAt != nil {
			fmt.Fprintf(&b, "  实际 %s", stamp(*j.SentAt, user.Timezone))
		}
		if j.LastErrorCode != nil {
			fmt.Fprintf(&b, "  原因 %s", *j.LastErrorCode)
			if *j.LastErrorCode == "no_private_chat" {
				b.WriteString("（请先私聊发送 /start 建立接收地址）")
			}
		}
		b.WriteString("\n")
		if j.Status == "pending" || j.Status == "failed" {
			fmt.Fprintf(&b, "  取消：/cancel job %s\n", id(j.ID))
		}
	}
	appendNext(&b, page.NextCursor, "/reminders "+filter)
	return b.String(), nil
}

func (h *Handler) commandRenew(ctx context.Context, user service.User, raw string) (string, error) {
	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", fail("用法：/renew <period_id> expected_cycle=1&date_precision=date&expires_at=2027-01-31[&amount=1000&currency=TWD]")
	}
	periodID, err := parseID(fields[0])
	if err != nil {
		return "", fail("有效期 ID 必须是正整数。")
	}
	in, err := parseRenewInput(strings.Join(fields[1:], " "), true)
	if err != nil {
		return "", err
	}
	renewal, err := h.svc.Renew(ctx, user.ID, periodID, in)
	if err != nil {
		return "", err
	}
	return renewalText(ctx, h.svc, user, renewal, "续期完成"), nil
}

func (h *Handler) commandCancel(ctx context.Context, user service.User, raw string) (string, error) {
	args := strings.Fields(raw)
	if len(args) == 0 {
		removed, err := h.svc.CancelDialog(ctx, user.ID)
		if err != nil {
			return "", err
		}
		if removed {
			return "已取消续期对话。", nil
		}
		return "当前没有进行中的续期对话。", nil
	}
	if len(args) == 2 && args[0] == "job" {
		jobID, err := parseID(args[1])
		if err != nil {
			return "", fail("任务 ID 必须是正整数。")
		}
		if err := h.svc.CancelJob(ctx, user.ID, jobID); err != nil {
			return "", err
		}
		return "任务已取消。", nil
	}
	if len(args) == 3 && args[0] == "rule" {
		ruleID, err := parseID(args[1])
		if err != nil {
			return "", fail("规则 ID 必须是正整数。")
		}
		revision, err := parseID(args[2])
		if err != nil {
			return "", fail("expected_revision 必须是正整数。")
		}
		if err := h.svc.DisableRule(ctx, user.ID, ruleID, revision); err != nil {
			return "", err
		}
		return "提醒规则已禁用。", nil
	}
	return "", fail("用法：/cancel | /cancel job <id> | /cancel rule <id> <expected_revision>")
}

func parseRenewInput(raw string, requireCycle bool) (service.RenewInput, error) {
	allowed := []string{"date_precision", "expires_at", "amount", "currency", "notes"}
	if requireCycle {
		allowed = append(allowed, "expected_cycle")
	}
	v, err := query(raw, allowed...)
	if err != nil {
		return service.RenewInput{}, err
	}
	in := service.RenewInput{
		DatePrecision: strings.TrimSpace(v.Get("date_precision")),
		ExpiresAt:     strings.TrimSpace(v.Get("expires_at")),
	}
	if in.DatePrecision == "" || in.ExpiresAt == "" {
		return service.RenewInput{}, fail("续期需要 date_precision=date|datetime 与 expires_at；例如 date_precision=date&expires_at=2027-01-31")
	}
	if requireCycle {
		cycle, err := requiredInt(v, "expected_cycle", 1)
		if err != nil {
			return service.RenewInput{}, err
		}
		in.ExpectedCycle = cycle
	}
	if s, ok := v["amount"]; ok {
		amount, err := strconv.ParseInt(strings.TrimSpace(s[0]), 10, 64)
		if err != nil || amount < 0 {
			return service.RenewInput{}, fail("amount 必须是大于等于 0 的整数（最小货币单位）。")
		}
		in.Amount = &amount
	}
	if s, ok := v["currency"]; ok {
		currency := strings.ToUpper(strings.TrimSpace(s[0]))
		in.Currency = &currency
	}
	if s, ok := v["notes"]; ok {
		note := s[0]
		in.Notes = &note
	}
	if (in.Amount == nil) != (in.Currency == nil) {
		return service.RenewInput{}, fail("amount 与 currency 必须同时提供。")
	}
	return in, nil
}

// query parses the trailing standard query string and enforces the shared
// encoding contract: one value per known key, no unknown keys, valid escapes.
func query(raw string, allowed ...string) (url.Values, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return url.Values{}, nil
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		return nil, fail("查询参数编码无效；空格用 +，& 等字符需百分号编码。")
	}
	permitted := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		permitted[key] = true
	}
	for key, values := range v {
		if !permitted[key] {
			return nil, fail("不支持或未知的参数：" + key + "。")
		}
		if len(values) != 1 {
			return nil, fail("参数不能重复：" + key + "。")
		}
	}
	return v, nil
}

func requiredInt(v url.Values, key string, minimum int64) (int64, error) {
	s, ok := v[key]
	if !ok {
		return 0, fail("缺少参数 " + key + "。")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s[0]), 10, 64)
	if err != nil || n < minimum {
		return 0, fail(key + " 必须是大于等于 " + strconv.FormatInt(minimum, 10) + " 的整数。")
	}
	return n, nil
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fail("enabled 只能是 true 或 false。")
	}
}

func optionalID(raw string) (int64, error) {
	args := strings.Fields(raw)
	if len(args) == 0 {
		return 0, nil
	}
	if len(args) > 1 {
		return 0, fail("用法：/list [after_id]")
	}
	return parseAfter(args[0])
}

func parseID(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 1 {
		return 0, fail("ID 必须是正整数。")
	}
	return n, nil
}

func parseAfter(raw string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 0 {
		return 0, fail("after_id 必须是非负整数。")
	}
	return n, nil
}

func optionalValue(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return &raw
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

func textOrDash(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func stamp(unix int64, zone string) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Unix(unix, 0).UTC().Format(time.RFC3339)
	}
	return time.Unix(unix, 0).In(loc).Format(time.RFC3339)
}

func simDetail(sim service.SIM) string {
	var b strings.Builder
	fmt.Fprintf(&b, "卡片 #%s\n名称：%s\n类型：%s\n状态：%s\n", id(sim.ID), sim.Name, sim.CardType, sim.Status)
	fmt.Fprintf(&b, "运营商：%s\n电话：%s\n国家/地区：%s\nICCID：%s\n", textOrDash(sim.Operator), textOrDash(sim.PhoneNumber), textOrDash(sim.CountryOrRegion), textOrDash(sim.ICCID))
	if sim.Notes != nil && *sim.Notes != "" {
		fmt.Fprintf(&b, "备注：%s\n", *sim.Notes)
	}
	return strings.TrimRight(b.String(), "\n")
}

func periodExpiry(p service.Period) string {
	if p.DatePrecision == domain.PrecisionDate && p.LocalExpiresDate != nil {
		return *p.LocalExpiresDate + "（" + p.Timezone + "，当日结束前有效）"
	}
	return stamp(p.ExpiresAt, p.Timezone) + "（" + p.Timezone + "）"
}

func periodDetail(p service.Period) string {
	var b strings.Builder
	fmt.Fprintf(&b, "有效期 #%s\n名称：%s\n类型：%s\n精度：%s\n截止：%s\n状态：%s\n版本：%d\n", id(p.ID), p.Name, p.PeriodType, p.DatePrecision, periodExpiry(p), p.Status, p.Cycle)
	if p.StartsAt != nil {
		fmt.Fprintf(&b, "开始：%s\n", stamp(*p.StartsAt, p.Timezone))
	}
	return strings.TrimRight(b.String(), "\n")
}

func ruleLine(r service.Rule) string {
	return fmt.Sprintf("规则 #%s：提前 %d 天 %s，启用 %t，版本 %d。\n修改：/edit rule %s expected_revision=%d ...\n禁用：/cancel rule %s %d",
		id(r.ID), r.DaysBefore, r.ReminderTime, r.Enabled, r.Revision, id(r.ID), r.Revision, id(r.ID), r.Revision)
}

func renewalText(ctx context.Context, svc *service.Service, user service.User, r service.Renewal, label string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s：有效期 #%s\n周期 %d → %d\n新截止：%s\n", label, id(r.ValidityPeriodID), r.PreviousCycle, r.PreviousCycle+1, stamp(r.NewExpiresAt, r.Timezone))
	if r.NewDatePrecision == domain.PrecisionDate && r.NewLocalExpiresDate != nil {
		fmt.Fprintf(&b, "新日期：%s（%s，当日结束前有效）\n", *r.NewLocalExpiresDate, r.Timezone)
	}
	if r.Amount != nil && r.Currency != nil {
		fmt.Fprintf(&b, "金额：%d %s（最小货币单位）\n", *r.Amount, *r.Currency)
	}
	if r.Notes != nil && *r.Notes != "" {
		fmt.Fprintf(&b, "备注：%s\n", *r.Notes)
	}
	if r.Replayed {
		b.WriteString("（该续期此前已完成，显示历史记录。）\n")
	}
	if p, err := svc.GetPeriod(ctx, user.ID, r.ValidityPeriodID); err == nil {
		fmt.Fprintf(&b, "当前状态：%s，版本 %d\n", p.Status, p.Cycle)
	}
	return strings.TrimRight(b.String(), "\n")
}

func appendNext(b *strings.Builder, next *int64, prefix string) {
	if next != nil {
		fmt.Fprintf(b, "\n下一页：%s %s", prefix, id(*next))
	}
}

func helpText(zone string) string {
	return `SIM/eSIM 有效期提醒机器人

当前时区：` + zone + `
命令（私聊使用）：
/start、/help — 帮助
/add name=日本卡&card_type=esim&operator=... — 新建卡片（card_type 为 esim 或 physical_sim）
/list [after_id] — 卡片列表（每页 10 条）
/view sim <id> [periods [after_id]] — 卡片详情/有效期列表
/view period <id> [rules|renewals [after_id]] — 有效期详情/规则/续期历史
/edit period new <sim_id> period_type=number_validity&name=名称&date_precision=date&expires_at=2027-01-31 — 新建有效期
/edit sim <id> <query> — 修改卡片
/edit period <id> expected_cycle=1&... — 修改有效期（cycle 变化会重新排程）
/edit rule new <period_id> days_before=7&reminder_time=09:00 — 新建提醒规则
/edit rule <id> expected_revision=1&... — 修改规则
/edit timezone Asia/Taipei — 修改时区
/delete sim <id> — 归档卡片
/delete period <id> <expected_cycle> — 取消有效期
/reminders [pending|sending|sent|failed|cancelled|all] [after_id] — 提醒任务
/renew <period_id> expected_cycle=1&date_precision=date&expires_at=2027-01-31[&amount=1000&currency=TWD&notes=...] — 确认续期
/cancel — 取消续期对话
/cancel job <id> — 取消单个任务
/cancel rule <id> <expected_revision> — 禁用规则

提醒示例：提前 30/7/3/1/0 天的 09:00。未配置规则的项目不会发送提醒。
编码：空格用 +，&、= 等字符需百分号编码；date 精度格式 YYYY-MM-DD，datetime 精度使用带时区的 RFC3339。`
}
