package service

import (
	"context"
	"testing"
)

// The primary picker must agree with the legacy overview and remain one row
// per service even when a service has many periods.
func TestServicePrimaryMatchesLegacyOrdering(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	category := stageTwoCategory(t, st, "other")
	item := stageTwoService(t, s, a.ID, category, "ties")
	var periods []Period
	for _, date := range []string{"2027-02-01", "2026-12-31", "2026-12-31"} {
		periods = append(periods, stageTwoPeriod(t, s, a.ID, item.ID, date, nil))
	}
	cancelled := stageTwoPeriod(t, s, a.ID, item.ID, "2026-01-01", nil)
	if err := s.CancelPeriod(ctx, a.ID, cancelled.ID, cancelled.Cycle); err != nil {
		t.Fatal(err)
	}
	overview, err := s.ServiceOverview(ctx, a.ID, 0, 10, ServiceFilter{Status: "all"})
	if err != nil || overview.Counts != (OverviewCounts{Total: 1, Expired: 1}) || len(overview.Items.Data) != 1 || overview.Items.Data[0].PrimaryPeriod == nil {
		t.Fatalf("overview %+v %v", overview, err)
	}
	if got := overview.Items.Data[0].PrimaryPeriod.ID; got != periods[2].ID {
		t.Fatalf("primary %d want %d", got, periods[2].ID)
	}
	page, err := s.ListServices(ctx, a.ID, 0, 10, ServiceFilter{Query: "ties"})
	if err != nil || len(page.Data) != 1 || page.Data[0].ID != item.ID {
		t.Fatalf("search %+v %v", page, err)
	}
	if _, err = s.ServiceOverview(ctx, a.ID, 0, 101, ServiceFilter{}); err != ErrInvalid {
		t.Fatalf("limit: %v", err)
	}
	if _, err = s.ListServices(ctx, a.ID, 0, 10, ServiceFilter{Query: string([]byte{0xff})}); err != ErrInvalid {
		t.Fatalf("invalid utf8: %v", err)
	}
}
