package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidInitData deliberately carries no credential or parser details.
var ErrInvalidInitData = errors.New("invalid init data")

// VerifyInitData verifies Telegram's HMAC scheme and the freshness window.
func VerifyInitData(raw, botToken string, now time.Time, maxAge time.Duration) (int64, error) {
	userID, issued, err := DecodeInitData(raw, botToken, now)
	if err != nil || !Fresh(issued, now, maxAge) {
		return 0, ErrInvalidInitData
	}
	return userID, nil
}

// DecodeInitData verifies Telegram's bot-token HMAC scheme, decoding query
// values exactly once. All received fields except hash participate in the MAC.
// Freshness is checked separately so the API can consult a per-user window.
func DecodeInitData(raw, botToken string, now time.Time) (int64, time.Time, error) {
	if len(raw) == 0 || len(raw) > 16*1024 || botToken == "" {
		return 0, time.Time{}, ErrInvalidInitData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return 0, time.Time{}, ErrInvalidInitData
	}
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if len(value) != 1 || key == "" {
			return 0, time.Time{}, ErrInvalidInitData
		}
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	mac, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(mac) != sha256.Size {
		return 0, time.Time{}, ErrInvalidInitData
	}
	sort.Strings(keys)
	var check strings.Builder
	for i, key := range keys {
		if i > 0 {
			check.WriteByte('\n')
		}
		check.WriteString(key)
		check.WriteByte('=')
		check.WriteString(values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	expected := hmac.New(sha256.New, secret.Sum(nil))
	expected.Write([]byte(check.String()))
	if !hmac.Equal(mac, expected.Sum(nil)) {
		return 0, time.Time{}, ErrInvalidInitData
	}
	date := values.Get("auth_date")
	if date == "" || strings.IndexFunc(date, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, time.Time{}, ErrInvalidInitData
	}
	seconds, err := strconv.ParseInt(date, 10, 64)
	if err != nil {
		return 0, time.Time{}, ErrInvalidInitData
	}
	issued := time.Unix(seconds, 0)
	if issued.After(now.Add(30 * time.Second)) {
		return 0, time.Time{}, ErrInvalidInitData
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return 0, time.Time{}, ErrInvalidInitData
	}
	return user.ID, issued, nil
}

// Fresh reports whether initData issued at the given time is inside maxAge,
// while still allowing the small positive clock-skew tolerance.
func Fresh(issued, now time.Time, maxAge time.Duration) bool {
	return maxAge > 0 && !issued.Before(now.Add(-maxAge)) && !issued.After(now.Add(30*time.Second))
}
