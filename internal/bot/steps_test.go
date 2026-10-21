package bot

import (
	"testing"

	"telegram.bot.remind/internal/service"
)

// The wizard step order is a product rule: a flow must ask the same
// questions in the same order for every user. These tests pin the whole graph
// so a change to the registry cannot silently reorder a flow.
func TestWizardStepGraph(t *testing.T) {
	draftOf := func(values map[string]string) draft {
		d := draft{Values: map[string]string{}}
		for key, value := range values {
			d.Values[key] = value
		}
		return d
	}
	sim := draftOf(map[string]string{"kind": "sim"})
	generic := draftOf(map[string]string{"kind": "generic"})
	recurring := draftOf(map[string]string{"mode": "recurring"})
	once := draftOf(map[string]string{"mode": "once"})

	// next maps a step to its successor and prev to its predecessor. An empty
	// successor or predecessor means the flow ends there.
	cases := []struct {
		name   string
		action string
		d      draft
		next   map[string]string
		prev   map[string]string
	}{
		{
			name:   "service add for a SIM category",
			action: service.DialogServiceAdd,
			d:      sim,
			next: map[string]string{
				"add_category": "add_name", "add_name": "add_card_type",
				"add_card_type": "add_provider", "add_provider": "add_management_url",
				"add_management_url": "add_phone", "add_phone": "add_region",
				"add_region": "add_iccid", "add_iccid": "add_notes",
				"add_notes": "add_preview",
			},
			prev: map[string]string{
				"add_name": "add_category", "add_card_type": "add_name",
				"add_provider": "add_card_type", "add_management_url": "add_provider",
				"add_phone": "add_management_url", "add_region": "add_phone",
				"add_iccid": "add_region", "add_notes": "add_iccid",
				"add_preview": "add_notes",
			},
		},
		{
			name:   "service add for a generic category",
			action: service.DialogServiceAdd,
			d:      generic,
			next: map[string]string{
				"add_category": "add_name", "add_name": "add_provider",
				"add_provider": "add_identifier", "add_identifier": "add_management_url",
				"add_management_url": "add_notes", "add_notes": "add_preview",
			},
			prev: map[string]string{
				"add_name": "add_category", "add_provider": "add_name",
				"add_identifier": "add_provider", "add_management_url": "add_identifier",
				"add_notes": "add_management_url", "add_preview": "add_notes",
			},
		},
		{
			name:   "period add with recurring terms",
			action: service.DialogPeriodAdd,
			d:      recurring,
			next: map[string]string{
				"period_name": "period_precision", "period_precision": "period_expires",
				"period_expires": "period_starts", "period_starts": "terms_mode",
				"terms_mode": "terms_interval", "terms_interval": "terms_amount",
				"terms_amount": "period_preview",
			},
			prev: map[string]string{
				"period_precision": "period_name", "period_expires": "period_precision",
				"period_starts": "period_expires", "terms_mode": "period_starts",
				"terms_interval": "terms_mode", "terms_amount": "terms_interval",
				"period_preview": "terms_amount",
			},
		},
		{
			name:   "period add with one-off terms",
			action: service.DialogPeriodAdd,
			d:      once,
			next: map[string]string{
				"period_name": "period_precision", "period_precision": "period_expires",
				"period_expires": "period_starts", "period_starts": "terms_mode",
				"terms_mode": "period_preview",
			},
			prev: map[string]string{
				"period_precision": "period_name", "period_expires": "period_precision",
				"period_starts": "period_expires", "terms_mode": "period_starts",
				"period_preview": "terms_mode",
			},
		},
		{
			name:   "service edit",
			action: service.DialogServiceEdit,
			d:      generic,
			next: map[string]string{
				"edit_field": "edit_value", "edit_value": "edit_preview",
			},
			prev: map[string]string{
				"edit_value": "edit_field", "edit_preview": "edit_value",
			},
		},
		{
			name:   "renew",
			action: service.DialogRenew,
			d:      generic,
			next: map[string]string{
				"renew_precision": "renew_expiration", "renew_expiration": "renew_amount",
				"renew_amount": "renew_notes", "renew_notes": "renew_preview",
			},
			prev: map[string]string{
				"renew_expiration": "renew_precision", "renew_amount": "renew_expiration",
				"renew_notes": "renew_amount", "renew_preview": "renew_notes",
			},
		},
		{
			name:   "rule add",
			action: service.DialogRuleAdd,
			d:      generic,
			next: map[string]string{
				"rule_days": "rule_time", "rule_time": "rule_preview",
			},
			prev: map[string]string{
				"rule_time": "rule_days", "rule_preview": "rule_time",
			},
		},
		{
			name:   "rule edit",
			action: service.DialogRuleEdit,
			d:      generic,
			next: map[string]string{
				"rule_days": "rule_time", "rule_time": "rule_preview",
			},
			prev: map[string]string{
				"rule_time": "rule_days", "rule_preview": "rule_time",
			},
		},
		{
			name:   "timezone",
			action: service.DialogTimezone,
			d:      generic,
			next: map[string]string{
				"tz_value": "tz_preview",
			},
			prev: map[string]string{
				"tz_preview": "tz_value",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for step, want := range tc.next {
				if got := nextStep(tc.action, tc.d, step); got != want {
					t.Errorf("next(%q)=%q want %q", step, got, want)
				}
			}
			for step, want := range tc.prev {
				if got := previousStep(tc.action, tc.d, step); got != want {
					t.Errorf("prev(%q)=%q want %q", step, got, want)
				}
			}
		})
	}
}

// The period edit flow branches on the field the user picked.
func TestPeriodEditStepBranchesOnField(t *testing.T) {
	draftOf := func(values map[string]string) draft {
		d := draft{Values: map[string]string{}}
		for key, value := range values {
			d.Values[key] = value
		}
		return d
	}
	cases := []struct {
		field string
		mode  string
		next  map[string]string
		prev  map[string]string
	}{
		{
			field: "name",
			next:  map[string]string{"edit_field": "edit_value", "edit_value": "edit_preview"},
			prev:  map[string]string{"edit_value": "edit_field", "edit_preview": "edit_value"},
		},
		{
			field: "starts",
			next:  map[string]string{"edit_field": "edit_value", "edit_value": "edit_preview"},
			prev:  map[string]string{"edit_value": "edit_field", "edit_preview": "edit_value"},
		},
		{
			field: "expires",
			next: map[string]string{
				"edit_field": "period_precision", "period_precision": "period_expires",
				"period_expires": "edit_preview",
			},
			prev: map[string]string{
				"period_precision": "edit_field", "period_expires": "period_precision",
				"edit_preview": "period_expires",
			},
		},
		{
			field: "terms",
			mode:  "once",
			next:  map[string]string{"edit_field": "terms_mode", "terms_mode": "edit_preview"},
			prev:  map[string]string{"terms_mode": "edit_field", "edit_preview": "terms_mode"},
		},
		{
			field: "terms",
			mode:  "recurring",
			next: map[string]string{
				"edit_field": "terms_mode", "terms_mode": "terms_interval",
				"terms_interval": "terms_amount", "terms_amount": "edit_preview",
			},
			prev: map[string]string{
				"terms_mode": "edit_field", "terms_interval": "terms_mode",
				"terms_amount": "terms_interval", "edit_preview": "terms_amount",
			},
		},
	}
	for _, tc := range cases {
		name := tc.field
		if tc.mode != "" {
			name += "/" + tc.mode
		}
		t.Run(name, func(t *testing.T) {
			d := draftOf(map[string]string{"field": tc.field, "mode": tc.mode})
			for step, want := range tc.next {
				if got := nextStep(service.DialogPeriodEdit, d, step); got != want {
					t.Errorf("next(%q)=%q want %q", step, got, want)
				}
			}
			for step, want := range tc.prev {
				if got := previousStep(service.DialogPeriodEdit, d, step); got != want {
					t.Errorf("prev(%q)=%q want %q", step, got, want)
				}
			}
		})
	}
}

// Every registered step must have a successor and a predecessor unless it ends
// the flow, and both directions must agree with the registry order.
func TestWizardStepGraphIsConsistent(t *testing.T) {
	for action, specs := range wizardSequences {
		for i, spec := range specs {
			if i == 0 && spec.prev != "" {
				t.Errorf("%s: first step %q has a predecessor %q", action, spec.key, spec.prev)
			}
			if i == len(specs)-1 && spec.next != "" {
				t.Errorf("%s: last step %q has a successor %q", action, spec.key, spec.next)
			}
			if spec.key == "" {
				t.Errorf("%s: empty step key", action)
			}
		}
	}
}
