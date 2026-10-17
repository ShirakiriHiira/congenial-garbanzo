package service

import (
	"context"
	"testing"
	"time"
)

func TestOverviewCountsAndPrimaryPeriod(t *testing.T) {
	s, _, a, b := setup(t)
	ctx := context.Background()

	soon, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "soon", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	for _, expires := range []string{"2027-01-20", "2027-01-08"} {
		if _, e = s.CreatePeriod(ctx, a.ID, soon.ID, PeriodInput{PeriodType: "plan_validity", Name: expires, DatePrecision: "date", ExpiresAt: expires}); e != nil {
			t.Fatal(e)
		}
	}
	latestTie, e := s.CreatePeriod(ctx, a.ID, soon.ID, PeriodInput{PeriodType: "custom", Name: "same cutoff, newer record", DatePrecision: "date", ExpiresAt: "2027-01-08"})
	if e != nil {
		t.Fatal(e)
	}
	// A cancelled period is never primary, even though it expires first.
	cancelled, e := s.CreatePeriod(ctx, a.ID, soon.ID, PeriodInput{PeriodType: "custom", Name: "cancelled", DatePrecision: "date", ExpiresAt: "2026-06-01"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CancelPeriod(ctx, a.ID, cancelled.ID, cancelled.Cycle); e != nil {
		t.Fatal(e)
	}

	expired, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "expired", CardType: "physical_sim"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreatePeriod(ctx, a.ID, expired.ID, PeriodInput{PeriodType: "number_validity", Name: "gone", DatePrecision: "date", ExpiresAt: "2026-12-31"}); e != nil {
		t.Fatal(e)
	}

	noPeriod, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "bare", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	archived, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "archived", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ArchiveSIM(ctx, a.ID, archived.ID); e != nil {
		t.Fatal(e)
	}
	// Eight calendar days out is outside the due-soon window.
	far, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "far", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreatePeriod(ctx, a.ID, far.ID, PeriodInput{PeriodType: "custom", Name: "far", DatePrecision: "date", ExpiresAt: "2027-01-09"}); e != nil {
		t.Fatal(e)
	}

	// Another user's cards must not leak into the counts.
	other, e := s.CreateSIM(ctx, b.ID, SIMInput{Name: "other", CardType: "esim"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreatePeriod(ctx, b.ID, other.ID, PeriodInput{PeriodType: "custom", Name: "other", DatePrecision: "date", ExpiresAt: "2026-12-31"}); e != nil {
		t.Fatal(e)
	}

	got, e := s.Overview(ctx, a.ID, 0, 20)
	if e != nil {
		t.Fatal(e)
	}
	want := OverviewCounts{Total: 4, DueSoon: 1, Expired: 1, NoPeriod: 1}
	if got.Counts != want {
		t.Fatalf("counts=%+v want=%+v", got.Counts, want)
	}
	if len(got.Items.Data) != 4 || got.Items.NextCursor != nil {
		t.Fatalf("items=%+v next=%v", got.Items.Data, got.Items.NextCursor)
	}
	var soonItem *OverviewItem
	for i := range got.Items.Data {
		if got.Items.Data[i].SIM.ID == soon.ID {
			soonItem = &got.Items.Data[i]
		}
	}
	if soonItem == nil || soonItem.PrimaryPeriod == nil {
		t.Fatal("missing primary period for due-soon card")
	}
	if soonItem.PrimaryPeriod.ID != latestTie.ID || soonItem.PrimaryPeriod.ExpiresAt != mustExpiry(t, "2027-01-08") || soonItem.PrimaryPeriod.Status != "active" {
		t.Fatalf("primary=%+v", *soonItem.PrimaryPeriod)
	}
	for _, item := range got.Items.Data {
		if item.SIM.ID == noPeriod.ID && item.PrimaryPeriod != nil {
			t.Fatal("card without periods has a primary period")
		}
		if item.SIM.ID == archived.ID {
			t.Fatal("archived card leaked into overview")
		}
	}
}

func TestOverviewPagination(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	var ids []int64
	for range 3 {
		sim, e := s.CreateSIM(ctx, a.ID, SIMInput{Name: "card", CardType: "esim"})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, sim.ID)
	}
	first, e := s.Overview(ctx, a.ID, 0, 2)
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items.Data) != 2 || first.Items.NextCursor == nil || *first.Items.NextCursor != ids[1] {
		t.Fatalf("first=%+v", first.Items)
	}
	second, e := s.Overview(ctx, a.ID, *first.Items.NextCursor, 2)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items.Data) != 1 || second.Items.NextCursor != nil || second.Items.Data[0].SIM.ID != ids[2] {
		t.Fatalf("second=%+v", second.Items)
	}
	if first.Counts != second.Counts || first.Counts.Total != 3 {
		t.Fatalf("counts changed across pages: %+v vs %+v", first.Counts, second.Counts)
	}
	if _, e = s.Overview(ctx, a.ID, 0, 0); e != ErrInvalid {
		t.Fatalf("limit 0: %v", e)
	}
}

func TestOverviewUsesOneInstantAcrossCutoff(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	cutoff := time.Date(2027, 1, 1, 16, 0, 0, 0, time.UTC)
	sim, err := s.CreateSIM(ctx, a.ID, SIMInput{Name: "midnight", CardType: "esim"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: "date", DatePrecision: "date", ExpiresAt: "2027-01-01"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		at     time.Time
		counts OverviewCounts
		state  string
	}{
		{"before", cutoff.Add(-time.Nanosecond), OverviewCounts{Total: 1, DueSoon: 1}, "active"},
		{"at", cutoff, OverviewCounts{Total: 1, Expired: 1}, "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.at
			calls := 0
			s.now = func() time.Time {
				calls++
				value := at
				at = cutoff.Add(time.Second)
				return value
			}
			got, err := s.Overview(ctx, a.ID, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("clock sampled %d times, want exactly 1", calls)
			}
			if got.Counts != tc.counts || got.Items.Data[0].PrimaryPeriod.Status != tc.state {
				t.Fatalf("counts=%+v period=%+v", got.Counts, got.Items.Data[0].PrimaryPeriod)
			}
		})
	}
}

func TestOverviewDateTimeClassification(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	s.now = func() time.Time { return time.Date(2027, 1, 1, 17, 0, 0, 0, time.UTC) }
	// Local time is 01:00 on Jan 2 in Asia/Taipei.
	for _, tc := range []struct {
		value  string
		counts OverviewCounts
		status string
	}{
		// Expired by instant, on the current local date.
		{"2027-01-02T00:30:00+08:00", OverviewCounts{Total: 1, Expired: 1}, "expired"},
		// Still ahead of the instant, same local date: day 0.
		{"2027-01-02T02:00:00+08:00", OverviewCounts{Total: 2, DueSoon: 1, Expired: 1}, "active"},
		// Local day +7 is inside the window even though it is more than 7*24h away.
		{"2027-01-09T23:59:00+08:00", OverviewCounts{Total: 3, DueSoon: 2, Expired: 1}, "active"},
		// Local day +8 is outside the window; its UTC date is still Jan 9.
		{"2027-01-10T00:01:00+08:00", OverviewCounts{Total: 4, DueSoon: 2, Expired: 1}, "active"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			sim, err := s.CreateSIM(ctx, a.ID, SIMInput{Name: tc.value, CardType: "esim"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: tc.value, DatePrecision: "datetime", ExpiresAt: tc.value}); err != nil {
				t.Fatal(err)
			}
			got, err := s.Overview(ctx, a.ID, 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			if got.Counts != tc.counts {
				t.Fatalf("counts=%+v want=%+v", got.Counts, tc.counts)
			}
			item := got.Items.Data[len(got.Items.Data)-1]
			if item.SIM.ID != sim.ID || item.PrimaryPeriod.Status != tc.status {
				t.Fatalf("item=%+v want status=%s", item, tc.status)
			}
		})
	}
}

func mustExpiry(t *testing.T, value string) int64 {
	t.Helper()
	exp, e := time.Parse(time.RFC3339, value+"T16:00:00Z")
	if e != nil {
		t.Fatal(e)
	}
	return exp.Unix()
}
