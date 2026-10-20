package bot

import (
	"strings"
	"testing"
)

// SIM services expose their public facts through sim_details, so the wizard
// never asks for the generic identifier and never renders it.
func TestSIMWizardOmitsIdentifier(t *testing.T) {
	f := newFixture(t)
	f.start(userA)
	sim := serviceCategoryID(t, f, "sim")
	f.send(userA, "/add")
	f.callback(userA, choiceCallback("category", id(sim)), nil)
	f.send(userA, "旅行卡")
	f.callback(userA, choiceCallback("card_type", "esim"), nil)
	f.callback(userA, cb("dialog", "skip"), nil)
	if text := f.stub.lastText(); strings.Contains(text, "公开标识") {
		t.Fatalf("SIM asked for identifier: %q", text)
	}
	if text := f.stub.lastText(); !strings.Contains(text, "管理链接") {
		t.Fatalf("SIM skipped to: %q", text)
	}
	f.callback(userA, cb("dialog", "skip_rest"), nil)
	if text := f.stub.lastText(); strings.Contains(text, "公开标识") {
		t.Fatalf("SIM preview shows identifier: %q", text)
	}
	f.callback(userA, cb("dialog", "confirm"), nil)
	if text := f.stub.lastText(); strings.Contains(text, "公开标识") {
		t.Fatalf("SIM detail shows identifier: %q", text)
	}

	created := f.createSIM("卡")
	f.callback(userA, cb("service", "edit", id(created.ID)), nil)
	if markup := markupOf(f); strings.Contains(markup, choiceCallback("field", "identifier")) {
		t.Fatalf("SIM edit menu offers identifier: %q", markup)
	}

	f.send(userA, "/cancel")
	generic := f.createService("Host", "vps")
	f.callback(userA, cb("service", "edit", id(generic.ID)), nil)
	if markup := markupOf(f); !strings.Contains(markup, choiceCallback("field", "identifier")) {
		t.Fatalf("generic edit menu lost identifier: %q", markup)
	}
}
