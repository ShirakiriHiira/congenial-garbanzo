package bot

import (
	"strings"
	"testing"
)

// serviceCategoryID resolves a built-in category by its stable migration code.
func serviceCategoryID(t *testing.T, f *fixture, code string) int64 {
	t.Helper()
	return serviceLookupInt(t, f, "SELECT id FROM service_categories WHERE code=?", code)
}

func serviceLookupInt(t *testing.T, f *fixture, query string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := f.store.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func serviceLookupString(t *testing.T, f *fixture, query string, args ...any) string {
	t.Helper()
	var v string
	if err := f.store.DB.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func serviceLastAnswer(t *testing.T, f *fixture) string {
	t.Helper()
	answers := f.stub.callsOf("answerCallbackQuery")
	if len(answers) == 0 {
		t.Fatal("callback was not answered")
	}
	return answers[len(answers)-1].form.Get("text")
}

func serviceMustContain(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("reply %q does not contain %q", text, want)
		}
	}
}
