package bot

import "testing"

// A choice encodes the field's index, so the field order is part of the
// callback contract. These tests pin the encoded values: an already-sent
// button must keep decoding to the same field.
func TestEditFieldCallbackEncoding(t *testing.T) {
	cases := []struct {
		key   string
		value string
		want  string
	}{
		{key: "field", value: "name", want: "v3:dialog:set:field:1"},
		{key: "field", value: "category_id", want: "v3:dialog:set:field:2"},
		{key: "field", value: "provider", want: "v3:dialog:set:field:3"},
		{key: "field", value: "identifier", want: "v3:dialog:set:field:4"},
		{key: "field", value: "management_url", want: "v3:dialog:set:field:5"},
		{key: "field", value: "notes", want: "v3:dialog:set:field:6"},
		{key: "field", value: "card_type", want: "v3:dialog:set:field:7"},
		{key: "field", value: "phone", want: "v3:dialog:set:field:8"},
		{key: "field", value: "region", want: "v3:dialog:set:field:9"},
		{key: "field", value: "iccid", want: "v3:dialog:set:field:10"},
		{key: "pfield", value: "name", want: "v3:dialog:set:pfield:1"},
		{key: "pfield", value: "expires", want: "v3:dialog:set:pfield:2"},
		{key: "pfield", value: "starts", want: "v3:dialog:set:pfield:3"},
		{key: "pfield", value: "terms", want: "v3:dialog:set:pfield:4"},
	}
	for _, tc := range cases {
		if got := choiceCallback(tc.key, tc.value); got != tc.want {
			t.Errorf("choiceCallback(%q,%q)=%q want %q", tc.key, tc.value, got, tc.want)
		}
	}
	// The encoded value must decode back to the original field.
	for _, tc := range cases {
		code := tc.want[len("v3:dialog:set:")+len(tc.key)+1:]
		got, ok := decodeChoice(tc.key, code)
		if !ok || got != tc.value {
			t.Errorf("decodeChoice(%q,%q)=(%q,%v) want (%q,true)", tc.key, code, got, ok, tc.value)
		}
	}
}

// Every listed field must be accepted by the wizard's own validation.
func TestEditFieldValidationMatchesTable(t *testing.T) {
	for _, field := range serviceEditFields {
		if !validEditField(field.key) {
			t.Errorf("validEditField(%q)=false", field.key)
		}
	}
	for _, field := range periodEditFields {
		if !validPeriodField(field.key) {
			t.Errorf("validPeriodField(%q)=false", field.key)
		}
	}
	if validEditField("unknown") || validPeriodField("unknown") {
		t.Error("an unknown field was accepted")
	}
}
