package service

import (
	"context"
	"testing"
)

func TestOverviewCountsBeyondMaximumPage(t *testing.T) {
	s, _, a, _ := setup(t)
	ctx := context.Background()
	for range 105 {
		if _, err := s.CreateSIM(ctx, a.ID, SIMInput{Name: "card", CardType: "esim"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Overview(ctx, a.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := OverviewCounts{Total: 105, NoPeriod: 105}
	if first.Counts != want || len(first.Items.Data) != 100 || first.Items.NextCursor == nil {
		t.Fatalf("first=%+v", first)
	}
	last, err := s.Overview(ctx, a.ID, *first.Items.NextCursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if last.Counts != want || len(last.Items.Data) != 5 || last.Items.NextCursor != nil {
		t.Fatalf("last=%+v", last)
	}
}
