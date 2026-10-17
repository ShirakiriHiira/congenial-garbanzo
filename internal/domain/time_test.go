package domain

import (
	"errors"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestParseExpiration(t *testing.T) {
	cases := []struct {
		name, precision, value, zone, want string
	}{
		{"TaipeiDate", PrecisionDate, "2027-01-31", "Asia/Taipei", "2027-01-31T16:00:00Z"},
		{"OffsetDateTime", PrecisionDateTime, "2027-01-31T12:30:00+08:00", "Asia/Taipei", "2027-01-31T04:30:00Z"},
		{"UTCDateTime", PrecisionDateTime, "2027-01-31T12:30:00Z", "Asia/Taipei", "2027-01-31T12:30:00Z"},
		{"LeapDay", PrecisionDate, "2028-02-29", "UTC", "2028-03-01T00:00:00Z"},
		{"SpringDayIs23Hours", PrecisionDate, "2027-03-14", "America/New_York", "2027-03-15T04:00:00Z"},
		{"FallDayIs25Hours", PrecisionDate, "2027-11-07", "America/New_York", "2027-11-08T05:00:00Z"},
		{"SkippedFollowingDate", PrecisionDate, "2011-12-29", "Pacific/Apia", "2011-12-30T10:00:00Z"},
		{"MidnightGapBoundary", PrecisionDate, "2018-11-03", "America/Sao_Paulo", "2018-11-04T03:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp, err := ParseExpiration(tc.precision, tc.value, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			assertInstant(t, exp.ExpiresAt, tc.want)
			if exp.DatePrecision != tc.precision || exp.Timezone != tc.zone {
				t.Fatalf("lost precision/timezone: %+v", exp)
			}
			if tc.precision == PrecisionDate {
				if exp.LocalExpiresDate == nil || *exp.LocalExpiresDate != tc.value {
					t.Fatalf("lost original date: %+v", exp)
				}
			} else if exp.LocalExpiresDate != nil {
				t.Fatal("datetime unexpectedly has a local date")
			}
		})
	}
}

func TestParseExpirationRejectsInvalidInput(t *testing.T) {
	cases := []struct{ precision, value, zone string }{
		{PrecisionDate, "2027-02-29", "Asia/Taipei"},
		{PrecisionDate, "2027-04-31", "UTC"},
		{PrecisionDate, "2027-1-01", "UTC"},
		{PrecisionDate, "2027-01-1", "UTC"},
		{PrecisionDate, " 2027-01-01", "UTC"},
		{PrecisionDate, "2027-01-01T00:00:00Z", "UTC"},
		{PrecisionDate, "2011-12-30", "Pacific/Apia"},
		{PrecisionDate, "2027-01-01", "Not/AZone"},
		{PrecisionDate, "2027-01-01", ""},
		{PrecisionDate, "2027-01-01", "Local"},
		{PrecisionDateTime, "2027-01-01T12:00:00", "UTC"},
		{PrecisionDateTime, "2027-01-01T1:00:00Z", "UTC"},
		{PrecisionDateTime, "2027-01-01T12:00:00+24:00", "UTC"},
		{PrecisionDateTime, "2027-01-01T12:00:00+08:60", "UTC"},
		{PrecisionDateTime, "2027-01-01T12:00:60Z", "UTC"},
		{PrecisionDateTime, "2027-01-01T12:00:00,5Z", "UTC"},
		{PrecisionDateTime, "2027-01-01 12:00:00Z", "UTC"},
		{PrecisionDateTime, "2027-01-01T12:00:00Z ", "UTC"},
		{"", "2027-01-01", "UTC"},
	}
	for _, tc := range cases {
		t.Run(tc.precision+"/"+tc.value+"/"+tc.zone, func(t *testing.T) {
			if _, err := ParseExpiration(tc.precision, tc.value, tc.zone); !errors.Is(err, ErrInvalidTime) {
				t.Fatalf("expected ErrInvalidTime, got %v", err)
			}
		})
	}
}

func TestReminderAt(t *testing.T) {
	cases := []struct {
		name, precision, value, zone string
		days                         int64
		clock, want                  string
	}{
		{"TaipeiPreviousDay", PrecisionDate, "2027-01-31", "Asia/Taipei", 1, "09:00", "2027-01-30T01:00:00Z"},
		{"TaipeiSameDay", PrecisionDate, "2027-01-31", "Asia/Taipei", 0, "09:00", "2027-01-31T01:00:00Z"},
		{"SpringGap", PrecisionDate, "2027-03-14", "America/New_York", 0, "02:30", "2027-03-14T07:00:00Z"},
		{"SpringGapBeginning", PrecisionDate, "2027-03-14", "America/New_York", 0, "02:00", "2027-03-14T07:00:00Z"},
		{"FallOverlapEarliest", PrecisionDate, "2027-11-07", "America/New_York", 0, "01:30", "2027-11-07T05:30:00Z"},
		{"SpringCalendarSubtraction", PrecisionDate, "2027-03-15", "America/New_York", 2, "09:00", "2027-03-13T14:00:00Z"},
		{"FallCalendarSubtraction", PrecisionDate, "2027-11-08", "America/New_York", 2, "09:00", "2027-11-06T13:00:00Z"},
		{"HalfHourGap", PrecisionDate, "2027-10-03", "Australia/Lord_Howe", 0, "02:15", "2027-10-02T15:30:00Z"},
		{"DatetimeUsesLocalDate", PrecisionDateTime, "2027-01-31T20:00:00Z", "Asia/Taipei", 1, "09:00", "2027-01-31T01:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp, err := ParseExpiration(tc.precision, tc.value, tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			at, err := ReminderAt(exp, tc.days, tc.clock)
			if err != nil {
				t.Fatal(err)
			}
			assertInstant(t, at, tc.want)
			// Resolving an ambiguous minute repeatedly must never alternate
			// between the two UTC occurrences.
			again, err := ReminderAt(exp, tc.days, tc.clock)
			if err != nil || !again.Equal(at) {
				t.Fatalf("unstable resolution: %v, %v", again, err)
			}
		})
	}
}

func TestReminderRejectsInvalidOrLateSchedule(t *testing.T) {
	exp, err := ParseExpiration(PrecisionDateTime, "2027-01-31T09:00:00+08:00", "Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	for _, clock := range []string{"09:00", "09:01", "23:59", "9:00", "24:00", "09:60", "09:00:00", " 08:00", ""} {
		if _, err := ReminderAt(exp, 0, clock); !errors.Is(err, ErrInvalidTime) {
			t.Errorf("clock %q: expected invalid schedule, got %v", clock, err)
		}
	}
	for _, days := range []int64{-1, 1<<63 - 1} {
		if _, err := ReminderAt(exp, days, "08:00"); !errors.Is(err, ErrInvalidTime) {
			t.Errorf("days %d: %v", days, err)
		}
	}
	at, err := ReminderAt(exp, 0, "08:59")
	if err != nil {
		t.Fatal(err)
	}
	assertInstant(t, at, "2027-01-31T00:59:00Z")
	apia, err := ParseExpiration(PrecisionDate, "2011-12-31", "Pacific/Apia")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReminderAt(apia, 1, "09:00"); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("skipped reminder date: %v", err)
	}
}

func TestParseStartsAt(t *testing.T) {
	end := time.Date(2027, 1, 31, 16, 0, 0, 0, time.UTC)
	start, err := ParseStartsAt("2027-01-31T23:59:59+08:00", end)
	if err != nil {
		t.Fatal(err)
	}
	assertInstant(t, start, "2027-01-31T15:59:59Z")
	for _, value := range []string{"", "2027-01-31", "2027-02-01T00:00:00+08:00", "2027-02-01T00:00:01+08:00"} {
		if _, err := ParseStartsAt(value, end); !errors.Is(err, ErrInvalidTime) {
			t.Errorf("start %q: %v", value, err)
		}
	}
}

func TestTimezoneRecalculation(t *testing.T) {
	original, err := ParseExpiration(PrecisionDate, "2027-01-31", "Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ParseExpiration(original.DatePrecision, *original.LocalExpiresDate, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	assertInstant(t, changed.ExpiresAt, "2027-02-01T05:00:00Z")
	if *changed.LocalExpiresDate != *original.LocalExpiresDate {
		t.Fatal("calendar date changed")
	}
	instant, err := ParseExpiration(PrecisionDateTime, "2027-01-31T23:00:00+08:00", "Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := ParseExpiration(instant.DatePrecision, instant.ExpiresAt.Format(time.RFC3339), "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if !moved.ExpiresAt.Equal(instant.ExpiresAt) {
		t.Fatal("absolute cutoff changed")
	}
	before, err := ReminderAt(instant, 0, "09:00")
	if err != nil {
		t.Fatal(err)
	}
	after, err := ReminderAt(moved, 0, "09:00")
	if err != nil {
		t.Fatal(err)
	}
	assertInstant(t, before, "2027-01-31T01:00:00Z")
	assertInstant(t, after, "2027-01-31T14:00:00Z")
}

func TestCalendarDaysUntil(t *testing.T) {
	now := time.Date(2027, 1, 1, 4, 30, 0, 0, time.UTC) // 12:30 in Taipei
	dateExp := func(t *testing.T, value, zone string) Expiration {
		t.Helper()
		exp, err := ParseExpiration(PrecisionDate, value, zone)
		if err != nil {
			t.Fatal(err)
		}
		return exp
	}
	dtExp := func(t *testing.T, value, zone string) Expiration {
		t.Helper()
		exp, err := ParseExpiration(PrecisionDateTime, value, zone)
		if err != nil {
			t.Fatal(err)
		}
		return exp
	}
	cases := []struct {
		name string
		exp  Expiration
		now  time.Time
		want int
	}{
		{"SameDay", dateExp(t, "2027-01-01", "Asia/Taipei"), now, 0},
		{"Tomorrow", dateExp(t, "2027-01-02", "Asia/Taipei"), now, 1},
		{"DueSoonBoundary", dateExp(t, "2027-01-08", "Asia/Taipei"), now, 7},
		{"OutsideDueSoon", dateExp(t, "2027-01-09", "Asia/Taipei"), now, 8},
		{"Yesterday", dateExp(t, "2026-12-31", "Asia/Taipei"), now, -1},
		{"AcrossMonth", dateExp(t, "2027-03-02", "Asia/Taipei"), time.Date(2027, 2, 26, 4, 0, 0, 0, time.UTC), 4},
		{"DateLineZone", dateExp(t, "2027-01-03", "Pacific/Kiritimati"), time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC), 2},
		{"SpringDstIsNot24h", dateExp(t, "2027-03-14", "America/New_York"), time.Date(2027, 3, 13, 5, 0, 0, 0, time.UTC), 1},
		{"FallDstIsNot24h", dateExp(t, "2027-11-07", "America/New_York"), time.Date(2027, 11, 6, 4, 0, 0, 0, time.UTC), 1},
		{"SpringJumpAcrossToday", dateExp(t, "2027-03-15", "America/New_York"), time.Date(2027, 3, 14, 4, 30, 0, 0, time.UTC), 2},
		{"FallRepeatAcrossToday", dateExp(t, "2027-11-08", "America/New_York"), time.Date(2027, 11, 7, 3, 30, 0, 0, time.UTC), 2},
		{"DistantFuture", dateExp(t, "9999-12-30", "UTC"), time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), 3652057},
		{"DistantPast", dateExp(t, "0001-01-01", "UTC"), time.Date(9999, 12, 30, 0, 0, 0, 0, time.UTC), -3652057},
		{"DateTimeUsesLocalDate", dtExp(t, "2027-01-03T00:30:00+08:00", "Asia/Taipei"), now, 2},
		{"DateTimeLateLocalDay", dtExp(t, "2027-01-02T23:30:00+08:00", "Asia/Taipei"), now, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.exp.Timezone)
			if err != nil {
				t.Fatal(err)
			}
			got, err := CalendarDaysUntil(tc.exp, tc.now, loc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("days=%d want=%d", got, tc.want)
			}
		})
	}
	if _, err := CalendarDaysUntil(Expiration{DatePrecision: PrecisionDate}, now, nil); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("missing timezone accepted: %v", err)
	}
	if _, err := CalendarDaysUntil(Expiration{DatePrecision: PrecisionDate}, now, time.UTC); !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("missing local date accepted: %v", err)
	}
}

func assertInstant(t *testing.T, got time.Time, want string) {
	t.Helper()
	if got.Location() != time.UTC || got.Format(time.RFC3339Nano) != want {
		t.Fatalf("got %s (%s), want %s UTC", got.Format(time.RFC3339Nano), got.Location(), want)
	}
}
