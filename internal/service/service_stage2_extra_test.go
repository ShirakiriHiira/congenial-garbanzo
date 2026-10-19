package service

import (
	"context"
	"testing"
)

// The primary picker prefers the earliest unexpired period and falls back to
// the most recently expired one, preferring the highest ID for equal cutoffs.
func TestServicePrimaryOrdering(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	category := stageTwoCategory(t, st, "other")
	item := stageTwoService(t, s, a.ID, category, "ties")
	expiredFirst := stageTwoPeriod(t, s, a.ID, item.ID, "2026-12-31", nil)
	stageTwoPeriod(t, s, a.ID, item.ID, "2026-12-31", nil)
	future := stageTwoPeriod(t, s, a.ID, item.ID, "2027-02-01", nil)
	overview, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "all"})
	if err != nil || overview.Counts != (OverviewCounts{Total: 1}) || len(overview.Items.Data) != 1 || overview.Items.Data[0].PrimaryPeriod == nil {
		t.Fatalf("overview %+v %v", overview, err)
	}
	if got := overview.Items.Data[0].PrimaryPeriod.ID; got != future.ID || got == expiredFirst.ID {
		t.Fatalf("primary %d want %d", got, future.ID)
	}
	if got := len(overview.Items.Data[0].Periods); got != 3 {
		t.Fatalf("period count %d want 3", got)
	}
	if got := overview.Items.Data[0].Periods[0].ID; got != future.ID {
		t.Fatalf("first period %d want %d", got, future.ID)
	}

	expiredService := stageTwoService(t, s, a.ID, category, "expired ties")
	stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-10-01", nil)
	stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-12-31", nil)
	latest := stageTwoPeriod(t, s, a.ID, expiredService.ID, "2026-12-31", nil)
	expired, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "expired"})
	if err != nil || expired.Counts != (OverviewCounts{Total: 1, Expired: 1}) || len(expired.Items.Data) != 1 || expired.Items.Data[0].PrimaryPeriod == nil {
		t.Fatalf("expired overview %+v %v", expired, err)
	}
	if got := expired.Items.Data[0].PrimaryPeriod.ID; got != latest.ID {
		t.Fatalf("expired primary %d want %d", got, latest.ID)
	}
	page, err := s.ListServices(ctx, a.ID, 0, 10, ServiceFilter{Query: "ties"})
	if err != nil || len(page.Data) != 2 {
		t.Fatalf("search %+v %v", page, err)
	}
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 101, ServiceFilter{}); err != ErrInvalid {
		t.Fatalf("limit: %v", err)
	}
	if _, err = s.ListServices(ctx, a.ID, 0, 10, ServiceFilter{Query: string([]byte{0xff})}); err != ErrInvalid {
		t.Fatalf("invalid utf8: %v", err)
	}
}
