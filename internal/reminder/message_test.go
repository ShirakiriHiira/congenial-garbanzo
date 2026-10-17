package reminder

import (
	"strings"
	"testing"
	"time"

	"telegram.bot.remind/internal/domain"
	"telegram.bot.remind/internal/storage"
)

// TestReminderTextShowsLocalCutoff ensures the message is plain text and uses
// the user-facing local date rather than an ambiguous UTC boundary.
func TestReminderTextShowsLocalCutoff(t *testing.T) {
	date := "2027-01-31"
	text := reminderText(&storage.ReminderDelivery{
		ID: 4, PeriodID: 9, SIMName: "日本卡", PeriodName: "号码有效期",
		Expiration: domain.Expiration{
			ExpiresAt:        time.Date(2027, 1, 31, 16, 0, 0, 0, time.UTC),
			DatePrecision:    domain.PrecisionDate,
			LocalExpiresDate: &date,
			Timezone:         "Asia/Taipei",
		},
	})
	for _, want := range []string{"日本卡", "号码有效期", "2027-01-31", "Asia/Taipei", "项目 ID：9"} {
		if !strings.Contains(text, want) {
			t.Fatalf("message %q missing %q", text, want)
		}
	}
	if strings.Contains(text, "16:00:00Z") {
		t.Fatalf("message leaked UTC cutoff: %q", text)
	}
}

func TestReminderTextFallsBackForBlankTitle(t *testing.T) {
	for _, name := range []string{"", " \t"} {
		text := reminderText(&storage.ReminderDelivery{PeriodName: name})
		if !strings.Contains(text, "项目：有效期\n") {
			t.Fatalf("blank title: %q", text)
		}
	}
}
