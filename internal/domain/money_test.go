package domain

import (
	"errors"
	"testing"
)

func TestFormatMoneyUsesCurrencyMinorUnits(t *testing.T) {
	cases := []struct {
		amount   int64
		currency string
		want     string
	}{
		{1030, "CNY", "10.3"},
		{1030, "USD", "10.3"},
		{1200, "CNY", "12"},
		{120, "JPY", "120"},
		{1234, "KWD", "1.234"},
		{1230, "KWD", "1.23"},
		{5, "USD", "0.05"},
		{0, "USD", "0"},
		{100000, "VND", "100000"},
	}
	for _, tc := range cases {
		if got := FormatMoney(tc.amount, tc.currency); got != tc.want {
			t.Errorf("FormatMoney(%d, %q)=%q want %q", tc.amount, tc.currency, got, tc.want)
		}
	}
}

func TestParseMoneyReturnsExactMinorUnits(t *testing.T) {
	cases := []struct {
		amount   string
		currency string
		want     int64
	}{
		{"10.3", "CNY", 1030},
		{"10.30", "USD", 1030},
		{"12", "CNY", 1200},
		{"1200", "JPY", 1200},
		{"1.234", "KWD", 1234},
		{"0.05", "USD", 5},
		{"0", "CNY", 0},
	}
	for _, tc := range cases {
		got, err := ParseMoney(tc.amount, tc.currency)
		if err != nil {
			t.Fatalf("ParseMoney(%q, %q): %v", tc.amount, tc.currency, err)
		}
		if got != tc.want {
			t.Errorf("ParseMoney(%q, %q)=%d want %d", tc.amount, tc.currency, got, tc.want)
		}
		if repr := FormatMoney(got, tc.currency); repr == "" {
			t.Errorf("empty representation for %d %s", got, tc.currency)
		} else if back, err := ParseMoney(repr, tc.currency); err != nil || back != got {
			t.Errorf("round trip %q -> %d -> %q -> %d (%v)", tc.amount, got, repr, back, err)
		}
	}
}

// A value the currency cannot represent exactly is rejected instead of being
// rounded, so a stored amount always reads back as what the user typed.
func TestParseMoneyRejectsInexactInput(t *testing.T) {
	cases := []struct {
		amount   string
		currency string
		want     error
	}{
		{"1.234", "CNY", ErrMoneyFraction},
		{"1.5", "JPY", ErrMoneyFraction},
		{"1.23456", "KWD", ErrMoneyFraction},
		{"-1", "CNY", ErrMoneyFormat},
		{"1,5", "CNY", ErrMoneyFormat},
		{"1.", "CNY", ErrMoneyFormat},
		{".5", "CNY", ErrMoneyFormat},
		{"", "CNY", ErrMoneyFormat},
		{"99999999999999999999", "USD", ErrMoneyRange},
	}
	for _, tc := range cases {
		if _, err := ParseMoney(tc.amount, tc.currency); !errors.Is(err, tc.want) {
			t.Errorf("ParseMoney(%q, %q)=%v want %v", tc.amount, tc.currency, err, tc.want)
		}
	}
}
