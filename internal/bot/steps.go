package bot

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"
)

// stepSpec describes one wizard step. Sequence and validation are fixed in
// code; a stored step name outside this registry aborts the flow instead of
// being interpreted.
type stepSpec struct {
	key      string
	optional bool
}

var wizardSequences = map[string][]stepSpec{
	service.DialogRenew: {
		{key: "renew_precision"},
		{key: "renew_expiration"},
		{key: "renew_amount", optional: true},
		{key: "renew_notes", optional: true},
		{key: "renew_preview"},
	},
	service.DialogServiceAdd: {
		{key: "add_category"},
		{key: "add_name"},
		{key: "add_card_type"},
		{key: "add_provider", optional: true},
		{key: "add_identifier", optional: true},
		{key: "add_management_url", optional: true},
		{key: "add_phone", optional: true},
		{key: "add_region", optional: true},
		{key: "add_iccid", optional: true},
		{key: "add_notes", optional: true},
		{key: "add_preview"},
	},
	service.DialogServiceEdit: {
		{key: "edit_field"},
		{key: "edit_value"},
		{key: "edit_preview"},
	},
	service.DialogPeriodAdd: {
		{key: "period_name", optional: true},
		{key: "period_precision"},
		{key: "period_expires"},
		{key: "period_starts", optional: true},
		{key: "terms_mode"},
		{key: "terms_interval"},
		{key: "terms_amount", optional: true},
		{key: "period_preview"},
	},
	service.DialogPeriodEdit: {
		{key: "edit_field"},
		{key: "edit_value"},
		{key: "period_precision"},
		{key: "period_expires"},
		{key: "terms_mode"},
		{key: "terms_interval"},
		{key: "terms_amount", optional: true},
		{key: "edit_preview"},
	},
	service.DialogRuleAdd: {
		{key: "rule_days"},
		{key: "rule_time"},
		{key: "rule_enabled"},
		{key: "rule_preview"},
	},
	service.DialogRuleEdit: {
		{key: "rule_days"},
		{key: "rule_time"},
		{key: "rule_enabled"},
		{key: "rule_preview"},
	},
	service.DialogTimezone: {
		{key: "tz_value"},
		{key: "tz_preview"},
	},
}

func sequence(action string) []stepSpec {
	return wizardSequences[action]
}

// nextStep follows the fixed sequence and the code-defined branches for
// SIM-only fields, period-edit fields, and recurring terms. It never derives a
// step name from user input.
func nextStep(action string, d draft, step string) string {
	switch action {
	case service.DialogPeriodAdd:
		switch step {
		case "period_name":
			return "period_precision"
		case "period_precision":
			return "period_expires"
		case "period_expires":
			return "period_starts"
		case "period_starts":
			return "terms_mode"
		case "terms_mode":
			if d.Values["mode"] == "recurring" {
				return "terms_interval"
			}
			return "period_preview"
		case "terms_interval":
			return "terms_amount"
		case "terms_amount":
			return "period_preview"
		}
		return ""
	case service.DialogPeriodEdit:
		switch step {
		case "edit_field":
			switch d.Values["field"] {
			case "name", "starts":
				return "edit_value"
			case "expires":
				return "period_precision"
			case "terms":
				return "terms_mode"
			}
			return ""
		case "edit_value", "period_expires":
			return "edit_preview"
		case "period_precision":
			return "period_expires"
		case "terms_mode":
			if d.Values["mode"] == "recurring" {
				return "terms_interval"
			}
			return "edit_preview"
		case "terms_interval":
			return "terms_amount"
		case "terms_amount":
			return "edit_preview"
		}
		return ""
	case service.DialogServiceAdd:
		switch step {
		case "add_category":
			return "add_name"
		case "add_name":
			if d.Values["kind"] == "sim" {
				return "add_card_type"
			}
			return "add_provider"
		case "add_card_type":
			return "add_provider"
		case "add_provider":
			return "add_identifier"
		case "add_identifier":
			return "add_management_url"
		case "add_management_url":
			if d.Values["kind"] == "sim" {
				return "add_phone"
			}
			return "add_notes"
		case "add_phone":
			return "add_region"
		case "add_region":
			return "add_iccid"
		case "add_iccid":
			return "add_notes"
		case "add_notes":
			return "add_preview"
		}
		return ""
	case service.DialogServiceEdit:
		if step == "edit_field" {
			return "edit_value"
		}
		if step == "edit_value" {
			return "edit_preview"
		}
		return ""
	}
	specs := sequence(action)
	for i, spec := range specs {
		if spec.key != step {
			continue
		}
		for _, next := range specs[i+1:] {
			if applies(action, d, next.key) {
				return next.key
			}
		}
	}
	return ""
}

func previousStep(action string, d draft, step string) string {
	switch action {
	case service.DialogPeriodAdd:
		prev := map[string]string{
			"period_precision": "period_name", "period_expires": "period_precision",
			"period_starts": "period_expires", "terms_mode": "period_starts",
			"terms_interval": "terms_mode", "terms_amount": "terms_interval",
			"period_preview": "terms_amount",
		}
		if step == "period_preview" && d.Values["mode"] != "recurring" {
			return "terms_mode"
		}
		return prev[step]
	case service.DialogPeriodEdit:
		switch step {
		case "edit_value", "period_precision", "terms_mode":
			return "edit_field"
		case "period_expires":
			return "period_precision"
		case "terms_interval":
			return "terms_mode"
		case "terms_amount":
			return "terms_interval"
		case "edit_preview":
			switch d.Values["field"] {
			case "name", "starts":
				return "edit_value"
			case "expires":
				return "period_expires"
			case "terms":
				if d.Values["mode"] == "recurring" {
					return "terms_amount"
				}
				return "terms_mode"
			}
		}
		return ""
	case service.DialogServiceAdd, service.DialogServiceEdit, service.DialogRuleAdd, service.DialogRuleEdit, service.DialogRenew, service.DialogTimezone:
		specs := sequence(action)
		for i, spec := range specs {
			if spec.key == step {
				for j := i - 1; j >= 0; j-- {
					if applies(action, d, specs[j].key) {
						return specs[j].key
					}
				}
				return ""
			}
		}
	}
	return ""
}

// applies hides the SIM-only questions for generic categories and the
// recurring-details questions for non-recurring terms.
func applies(action string, d draft, step string) bool {
	switch step {
	case "add_card_type", "add_phone", "add_region", "add_iccid":
		return d.Values["kind"] == "sim"
	case "terms_interval", "terms_amount":
		return d.Values["mode"] == "recurring"
	}
	return true
}

func optionalStep(step string) bool {
	for _, specs := range wizardSequences {
		for _, spec := range specs {
			if spec.key == step {
				return spec.optional
			}
		}
	}
	return false
}

// localDateTimePattern matches a wall-clock timestamp without an offset, for
// example "2027-02-28 23:59" or "2027-02-28T23:59:30".
var localDateTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}(:[0-9]{2})?$`)

const dateTimeHelp = "精确时间请发送本地时间，例如 2027-02-28 23:59（按你的时区）；也接受带 offset 的 RFC3339。"

// normalizeDateTime turns an offset-qualified RFC3339 timestamp or a local
// wall-clock timestamp in zone into RFC3339 with the zone's offset. Drafts and
// the service layer always receive an absolute instant, so the user never has
// to type or look up an offset; a wall time skipped by a DST gap is rejected
// instead of silently moving.
func normalizeDateTime(value, zone string) (string, error) {
	if localDateTimePattern.MatchString(value) {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return "", fail("时区无效，请先在设置中重新选择时区。")
		}
		at, err := domain.ParseLocalDateTime(value, zone)
		if err != nil {
			return "", fail("该本地时间在 " + zone + " 不存在（日期无效或因夏令时跳过）。" + dateTimeHelp)
		}
		return at.In(loc).Format(time.RFC3339), nil
	}
	// time.RFC3339 parsing requires an explicit offset, so a successful parse
	// already proves the value is unambiguous.
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return "", fail(dateTimeHelp)
	}
	return value, nil
}

// normalizePrecisionValue validates the shared date/datetime contract locally
// and returns the canonical value the service layer expects.
func normalizePrecisionValue(precision, value, zone string) (string, error) {
	value = strings.TrimSpace(value)
	switch precision {
	case "date":
		at, err := time.Parse("2006-01-02", value)
		if err != nil || at.Format("2006-01-02") != value {
			return "", fail("日期必须是存在的 YYYY-MM-DD，例如 2027-01-31。")
		}
		return value, nil
	case "datetime":
		return normalizeDateTime(value, zone)
	}
	return "", fail("精度必须是 date 或 datetime。")
}

// expirationHint renders the cutoff example for the selected precision, so a
// prompt never shows a format the current step would reject.
func expirationHint(precision string) string {
	if precision == "datetime" {
		return "示例：2027-02-28 23:59（精确时间，按你的时区）"
	}
	return "示例：2027-02-28（仅日期）"
}

// wallClockDisplay renders a date or an absolute instant as local wall time in
// zone, so previews never ask the user to read a UTC offset.
func wallClockDisplay(value, zone string) string {
	if localDateTimePattern.MatchString(value) {
		return strings.Replace(value, "T", " ", 1)
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return value
	}
	return at.In(loc).Format("2006-01-02 15:04")
}

func parseMoney(amountText, currencyText string) (*int64, *string, error) {
	amountText, currencyText = strings.TrimSpace(amountText), strings.ToUpper(strings.TrimSpace(currencyText))
	if amountText == "" && currencyText == "" {
		return nil, nil, nil
	}
	if amountText == "" || currencyText == "" {
		return nil, nil, fail("金额与币种必须同时提供或同时留空。")
	}
	var amount int64
	for _, r := range amountText {
		if r < '0' || r > '9' {
			return nil, nil, fail("金额必须是大于等于 0 的整数（最小货币单位）。")
		}
	}
	if len(amountText) > 18 {
		return nil, nil, fail("金额超出支持范围。")
	}
	for _, r := range amountText {
		amount = amount*10 + int64(r-'0')
	}
	if len(currencyText) != 3 {
		return nil, nil, fail("币种必须是三位大写字母，例如 USD。")
	}
	for _, r := range currencyText {
		if r < 'A' || r > 'Z' {
			return nil, nil, fail("币种必须是三位大写字母，例如 USD。")
		}
	}
	return &amount, &currencyText, nil
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

// choiceValues maps a stable decimal callback code to its enum value. The
// reverse map keeps callback payloads free of labels and user input.
var choiceValues = map[string]map[string]string{
	"precision": {"1": "date", "2": "datetime"},
	"card_type": {"1": "physical_sim", "2": "esim"},
	"mode":      {"1": "once", "2": "recurring", "3": "custom"},
	"enabled":   {"0": "false", "1": "true"},
}

var choiceCodes = func() map[string]map[string]string {
	out := map[string]map[string]string{}
	for key, values := range choiceValues {
		out[key] = map[string]string{}
		for code, value := range values {
			out[key][value] = code
		}
	}
	return out
}()

var serviceEditFields = []string{"name", "category_id", "provider", "identifier", "management_url", "notes", "card_type", "phone", "region", "iccid"}
var periodEditFields = []string{"name", "expires", "starts", "terms"}

// choiceCallback encodes a wizard option as v3:dialog:set:<key>:<code>.
func choiceCallback(key, value string) string {
	if codes, ok := choiceCodes[key]; ok {
		return cb("dialog", "set", key, codes[value])
	}
	switch key {
	case "category":
		return cb("dialog", "set", key, value)
	case "field":
		for i, field := range serviceEditFields {
			if field == value {
				return cb("dialog", "set", key, strconv.Itoa(i+1))
			}
		}
	case "pfield":
		for i, field := range periodEditFields {
			if field == value {
				return cb("dialog", "set", key, strconv.Itoa(i+1))
			}
		}
	case "tz":
		for i, zone := range timezoneSamples {
			if zone == value {
				return cb("dialog", "set", key, strconv.Itoa(i+1))
			}
		}
	}
	return cb("dialog", "set", key, value)
}

func decodeChoice(key, code string) (string, bool) {
	switch key {
	case "category":
		if _, err := strconv.ParseInt(code, 10, 64); err != nil {
			return "", false
		}
		return code, true
	case "field":
		n, err := strconv.Atoi(code)
		if err != nil || n < 1 || n > len(serviceEditFields) {
			return "", false
		}
		return serviceEditFields[n-1], true
	case "pfield":
		n, err := strconv.Atoi(code)
		if err != nil || n < 1 || n > len(periodEditFields) {
			return "", false
		}
		return periodEditFields[n-1], true
	case "tz":
		n, err := strconv.Atoi(code)
		if err != nil || n < 1 || n > len(timezoneSamples) {
			return "", false
		}
		return timezoneSamples[n-1], true
	default:
		if values, ok := choiceValues[key]; ok {
			value, ok := values[code]
			return value, ok
		}
	}
	return "", false
}

// choiceKeys maps button value names to draft keys. Buttons never carry a
// draft key directly, so a crafted callback cannot create arbitrary state.
var choiceKeys = map[string]string{
	"precision": "precision",
	"category":  "category_id",
	"card_type": "card_type",
	"mode":      "mode",
	"enabled":   "enabled",
	"field":     "field",
	"pfield":    "field",
	"tz":        "tz",
}

func (w *wizard) setChoice(ctx context.Context, key, value string) {
	draftKey, ok := choiceKeys[key]
	if !ok {
		w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
		return
	}
	value, ok = decodeChoice(key, value)
	if !ok {
		w.failure(ctx, "该按钮无效，请重新打开流程。", false, false)
		return
	}
	if draftKey == "field" {
		if w.d.Action == service.DialogPeriodEdit {
			if !validPeriodField(value) {
				w.failure(ctx, "该字段不可编辑，请重新打开流程。", false, false)
				return
			}
		} else if !validEditField(value) {
			w.failure(ctx, "该字段不可编辑，请重新打开流程。", false, false)
			return
		}
	}
	w.dr.set(draftKey, value)

	next := nextStep(w.d.Action, w.dr, w.d.Step)
	switch draftKey {
	case "precision":
		if value != "date" && value != "datetime" {
			w.failure(ctx, "精度只能是 date 或 datetime。", false, false)
			return
		}
	case "card_type":
		if value != "esim" && value != "physical_sim" {
			w.failure(ctx, "卡类型无效，请重新打开流程。", false, false)
			return
		}
	case "mode":
		if value != "once" && value != "recurring" && value != "custom" {
			w.failure(ctx, "条款模式无效，请重新打开流程。", false, false)
			return
		}
	case "enabled":
		if value != "true" && value != "false" {
			w.failure(ctx, "启用状态无效，请重新打开流程。", false, false)
			return
		}
	case "tz":
		found := false
		for _, zone := range timezoneSamples {
			if zone == value {
				found = true
			}
		}
		if !found {
			w.failure(ctx, "时区示例无效，请重新打开流程。", false, false)
			return
		}
	case "category_id":
		found, kind := false, "generic"
		for _, c := range mustCategories(ctx, w.h, w.user.ID) {
			if id(c.ID) == value {
				found, kind = true, c.Kind
			}
		}
		if !found {
			w.failure(ctx, "类别不存在或不可用，请重新打开流程。", false, false)
			return
		}
		w.dr.set("kind", kind)
	}
	if w.d.Step == "edit_field" {
		next = nextStep(w.d.Action, w.dr, w.d.Step)
	}
	if next == "" {
		w.failure(ctx, "该选择无效，请重新打开流程。", false, false)
		return
	}
	if !w.persist(ctx, next) {
		return
	}
	w.render(ctx)
}

func validEditField(field string) bool {
	switch field {
	case "name", "category_id", "provider", "identifier", "management_url", "notes", "card_type", "phone", "region", "iccid":
		return true
	}
	return false
}

func validPeriodField(field string) bool {
	switch field {
	case "name", "expires", "starts", "terms":
		return true
	}
	return false
}
