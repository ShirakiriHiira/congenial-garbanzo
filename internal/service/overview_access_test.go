package service

import (
	"context"
	"testing"
)

func TestOverviewEmptyCancelledAndRevoked(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	empty, err := s.Overview(ctx, a.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Counts != (OverviewCounts{}) || len(empty.Items.Data) != 0 || empty.Items.NextCursor != nil {
		t.Fatalf("empty overview=%+v", empty)
	}
	sim, err := s.CreateSIM(ctx, a.ID, SIMInput{Name: "cancelled-only", CardType: "esim"})
	if err != nil {
		t.Fatal(err)
	}
	period, err := s.CreatePeriod(ctx, a.ID, sim.ID, PeriodInput{PeriodType: "custom", Name: "cancelled", DatePrecision: "date", ExpiresAt: "2026-12-31"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CancelPeriod(ctx, a.ID, period.ID, period.Cycle); err != nil {
		t.Fatal(err)
	}
	got, err := s.Overview(ctx, a.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts != (OverviewCounts{Total: 1, NoPeriod: 1}) || len(got.Items.Data) != 1 || got.Items.Data[0].PrimaryPeriod != nil {
		t.Fatalf("cancelled-only overview=%+v", got)
	}
	s.allowed = func(int64) bool { return false }
	if _, err = s.Overview(ctx, a.ID, 0, 20); err != ErrForbidden {
		t.Fatalf("revoked access=%v", err)
	}
}
