package domain

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ErrMoneyFormat, ErrMoneyFraction and ErrMoneyRange classify money parsing
// failures so callers can render their own wording.
var (
	ErrMoneyFormat   = errors.New("money must be a non-negative decimal")
	ErrMoneyFraction = errors.New("too many fraction digits for the currency")
	ErrMoneyRange    = errors.New("money out of range")
)

// currencyMinorUnits lists the ISO 4217 currencies whose minor-unit exponent
// differs from the common two decimals. Stored amounts are always integers in
// minor units, so every renderer needs this table to show a readable value.
var currencyMinorUnits = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0,
	"XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
	"CLF": 4, "UYW": 4,
}

var moneyPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// CurrencyExponent returns the ISO 4217 minor-unit exponent of currency, so a
// stored integer can be converted to its major-unit decimal form.
func CurrencyExponent(currency string) int {
	if exponent, ok := currencyMinorUnits[currency]; ok {
		return exponent
	}
	return 2
}

// ParseMoney converts a major-unit decimal amount into exact minor units for
// the currency. The input must be a non-negative decimal with at most the
// currency's minor-unit digits, so no rounding ever happens.
func ParseMoney(amount, currency string) (int64, error) {
	if !moneyPattern.MatchString(amount) {
		return 0, ErrMoneyFormat
	}
	whole, fraction, _ := strings.Cut(amount, ".")
	exponent := CurrencyExponent(currency)
	if len(fraction) > exponent {
		return 0, ErrMoneyFraction
	}
	digits := whole + fraction + strings.Repeat("0", exponent-len(fraction))
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, ErrMoneyRange
	}
	return value, nil
}

// FormatMoney renders a stored minor-unit amount as its major-unit decimal
// form without trailing zeros, e.g. 1030 CNY as "10.3" and 1000000 JPY as
// "1000000". The currency itself is not appended; callers decide the context.
func FormatMoney(amount int64, currency string) string {
	sign := ""
	if amount < 0 {
		sign, amount = "-", -amount
	}
	exponent := CurrencyExponent(currency)
	if exponent == 0 {
		return sign + strconv.FormatInt(amount, 10)
	}
	scale := int64(1)
	for range exponent {
		scale *= 10
	}
	whole, fraction := strconv.FormatInt(amount/scale, 10), strconv.FormatInt(amount%scale, 10)
	fraction = strings.Repeat("0", exponent-len(fraction)) + fraction
	if trimmed := strings.TrimRight(fraction, "0"); trimmed != "" {
		return sign + whole + "." + trimmed
	}
	return sign + whole
}
