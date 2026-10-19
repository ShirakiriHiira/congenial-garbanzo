package bot

import (
	"fmt"
	"strings"

	"telegram.bot.remind/internal/service"
)

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
