package bot

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

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
		{key: "renew_choose_precision"},
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
		{key: "add_phone", optional: true},
		{key: "add_region", optional: true},
		{key: "add_iccid", optional: true},
		{key: "add_identifier", optional: true},
		{key: "add_management_url", optional: true},
		{key: "add_notes", optional: true},
		{key: "add_preview"},
	},
	service.DialogServiceEdit: {
		{key: "edit_field"},
		{key: "edit_value"},
		{key: "edit_preview"},
	},
	service.DialogPeriodAdd: {
		{key: "period_type"},
		{key: "period_name", optional: true},
		{key: "period_precision"},
		{key: "period_expires"},
		{key: "period_starts", optional: true},
		{key: "period_preview"},
	},
	service.DialogTermsEdit: {
		{key: "terms_mode"},
		{key: "terms_interval"},
		{key: "terms_amount", optional: true},
		{key: "terms_preview"},
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

func stepIndex(action, step string) int {
	for i, spec := range wizardSequences[action] {
		if spec.key == step {
			return i
		}
	}
	return -1
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

// nextStep skips steps that do not apply to the chosen category/kind. It
// never derives a step name from user input.
func nextStep(action string, d draft, step string) string {
	specs := wizardSequences[action]
	i := stepIndex(action, step)
	if i < 0 {
		return ""
	}
	for _, spec := range specs[i+1:] {
		if applies(action, d, spec.key) {
			return spec.key
		}
	}
	return ""
}

func previousStep(action string, d draft, step string) string {
	specs := wizardSequences[action]
	i := stepIndex(action, step)
	if i < 0 {
		return ""
	}
	for j := i - 1; j >= 0; j-- {
		if applies(action, d, specs[j].key) {
			return specs[j].key
		}
	}
	return ""
}

// applies hides the SIM-only questions for generic categories and the
// recurring-details question for non-recurring terms.
func applies(action string, d draft, step string) bool {
	switch step {
	case "add_card_type", "add_phone", "add_region", "add_iccid":
		return d.Values["kind"] == "sim"
	case "add_identifier", "add_management_url":
		return d.Values["kind"] != "sim"
	case "terms_interval":
		return d.Values["mode"] == "recurring"
	case "terms_amount":
		return d.Values["mode"] != ""
	}
	return true
}

var datePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// parsePrecisionValue validates the shared date/datetime contract locally so
// the wizard can explain the exact format before the service call.
func parsePrecisionValue(precision, value string) error {
	value = strings.TrimSpace(value)
	switch precision {
	case "date":
		at, err := time.Parse("2006-01-02", value)
		if err != nil || at.Format("2006-01-02") != value {
			return fail("日期必须是存在的 YYYY-MM-DD，例如 2027-01-31。")
		}
		return nil
	case "datetime":
		at, err := time.Parse(time.RFC3339, value)
		if err != nil || datePattern.MatchString(value) {
			return fail("时间必须是带时区的 RFC3339，例如 2027-01-31T23:59:59+08:00。")
		}
		if _, offset := at.Zone(); offset == 0 && !strings.HasSuffix(value, "Z") && !strings.Contains(value, "+00:00") && !strings.Contains(value, "-00:00") {
			return fail("时间必须带 offset，例如 2027-01-31T23:59:59+08:00。")
		}
		return nil
	}
	return fail("精度必须是 date 或 datetime。")
}

// expiration examples are rendered everywhere a user must type a cutoff.
const expirationHelp = "示例：\n2027-02-28（date）\n2027-02-28T23:59:59+08:00（datetime，须带时区 offset）"

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

// choiceValues maps a stable decimal callback code to its enum value. The
// reverse map keeps callback payloads free of labels and user input.
var choiceValues = map[string]map[string]string{
	"precision":   {"1": "date", "2": "datetime"},
	"card_type":   {"1": "physical_sim", "2": "esim"},
	"mode":        {"1": "once", "2": "recurring", "3": "custom"},
	"enabled":     {"0": "false", "1": "true"},
	"period_type": {"1": "number_validity", "2": "plan_validity", "3": "data_package_validity", "4": "custom"},
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

var editFieldOrder = []string{"name", "category_id", "provider", "identifier", "management_url", "notes", "card_type", "phone", "region", "iccid"}

// choiceCallback encodes a wizard option as v2:dialog:set:<key>:<code>.
func choiceCallback(key, value string) string {
	if codes, ok := choiceCodes[key]; ok {
		return cb("dialog", "set", key, codes[value])
	}
	switch key {
	case "category":
		return cb("dialog", "set", key, value)
	case "field":
		for i, field := range editFieldOrder {
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
		return code, true
	case "field":
		n, err := strconv.Atoi(code)
		if err != nil || n < 1 || n > len(editFieldOrder) {
			return "", false
		}
		return editFieldOrder[n-1], true
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
	"precision":   "precision",
	"category":    "category_id",
	"card_type":   "card_type",
	"period_type": "period_type",
	"mode":        "mode",
	"enabled":     "enabled",
	"field":       "field",
	"tz":          "tz",
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
	if draftKey == "field" && !validEditField(value) {
		w.failure(ctx, "该字段不可编辑，请重新打开流程。", false, false)
		return
	}
	w.dr.set(draftKey, value)

	next := nextStep(w.d.Action, w.dr, w.d.Step)
	if w.d.Step == "edit_field" {
		next = "edit_value"
	}
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
	case "period_type":
		switch value {
		case "number_validity", "plan_validity", "data_package_validity", "custom":
		default:
			w.failure(ctx, "有效期类型无效，请重新打开流程。", false, false)
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
	if w.d.Action == service.DialogServiceAdd && w.d.Step == "add_category" {
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
