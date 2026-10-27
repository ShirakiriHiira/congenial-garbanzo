package bot

import (
	"fmt"
	"strings"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/service"
)

func termsText(t service.PeriodTerms) string {
	var b strings.Builder
	switch t.Mode {
	case "once":
		b.WriteString("约定条款：一次性")
	case "recurring":
		b.WriteString("约定条款：周期性")
		if t.IntervalCount != nil && t.IntervalUnit != nil {
			fmt.Fprintf(&b, "，每 %d %s", *t.IntervalCount, intervalUnitLabel(*t.IntervalUnit))
		}
	default:
		b.WriteString("约定条款：暂未设置")
	}
	if t.Amount != nil && t.Currency != nil {
		fmt.Fprintf(&b, "，约定费用 %s %s（非已付款）", domain.FormatMoney(*t.Amount, *t.Currency), *t.Currency)
	}
	b.WriteString("；仅记录，不自动扣款或推进截止。")
	return b.String()
}

func intervalUnitLabel(unit string) string {
	switch unit {
	case "day":
		return "天"
	case "week":
		return "周"
	case "month":
		return "个月"
	case "year":
		return "年"
	}
	return unit
}
