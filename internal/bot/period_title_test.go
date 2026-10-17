package bot

import (
	"strings"
	"testing"

	"telegram.bot.remind/internal/service"
)

func TestPeriodDisplayFallsBackForBlankTitle(t *testing.T) {
	for _, name := range []string{"", " \t"} {
		p := service.Period{ID: 1, Name: name, PeriodType: "custom", DatePrecision: "datetime", Timezone: "UTC", Status: "active", Cycle: 1}
		if got := periodName(p); got != "有效期" {
			t.Fatalf("blank name: %q", got)
		}
		if got := periodDetail(p); !strings.Contains(got, "名称：有效期\n") {
			t.Fatalf("blank detail: %q", got)
		}
	}
	if got := periodName(service.Period{Name: "自定义标题"}); got != "自定义标题" {
		t.Fatalf("named period changed: %q", got)
	}
}
