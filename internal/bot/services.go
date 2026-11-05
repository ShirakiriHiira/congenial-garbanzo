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
		b.WriteString("续费信息：一次性")
	case "recurring":
		b.WriteString("续费信息：按周期")
		if t.IntervalCount != nil && t.IntervalUnit != nil {
			fmt.Fprintf(&b, "，每 %d %s", *t.IntervalCount, intervalUnitLabel(*t.IntervalUnit))
		}
	default:
		b.WriteString("续费信息：不记录")
	}
	if t.Amount != nil && t.Currency != nil {
		fmt.Fprintf(&b, "，费用 %s %s", domain.FormatMoney(*t.Amount, *t.Currency), *t.Currency)
	}
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
