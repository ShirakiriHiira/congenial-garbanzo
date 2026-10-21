package service

import (
	"context"
	"testing"
)

// A SIM service carries its public facts in SIMDetails; the generic identifier
// is reserved for the other categories.
func TestSIMServiceRejectsIdentifier(t *testing.T) {
	s, st, a, _ := setup(t)
	ctx := context.Background()
	simCategory := testSIMCategory(t, s)
	identifier := "should-not-stick"

	created, err := s.CreateService(ctx, a.ID, ServiceInput{
		Name: "卡", CategoryID: simCategory, SIMDetails: &SIMDetails{CardType: "esim"}, Identifier: &identifier,
	})
	if err != ErrInvalid || created.ID != 0 {
		t.Fatalf("create with identifier: item=%+v err=%v", created, err)
	}

	item, err := s.CreateService(ctx, a.ID, ServiceInput{Name: "卡", CategoryID: simCategory, SIMDetails: &SIMDetails{CardType: "esim"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateService(ctx, a.ID, item.ID, ServicePatch{Identifier: OptionalString{Set: true, Value: &identifier}}); err != ErrInvalid {
		t.Fatalf("patch identifier: %v", err)
	}
	// Clearing an absent identifier stays a no-op instead of an error, so an
	// outdated client can still save the rest of the form.
	updated, err := s.UpdateService(ctx, a.ID, item.ID, ServicePatch{Identifier: OptionalString{Set: true}})
	if err != nil || updated.Identifier != nil {
		t.Fatalf("clear identifier: item=%+v err=%v", updated, err)
	}

	vpsCategory := stageTwoCategory(t, st, "vps")
	generic, err := s.CreateService(ctx, a.ID, ServiceInput{Name: "云主机", CategoryID: vpsCategory, Identifier: &identifier})
	if err != nil || generic.Identifier == nil || *generic.Identifier != identifier {
		t.Fatalf("generic identifier: item=%+v err=%v", generic, err)
	}
}
