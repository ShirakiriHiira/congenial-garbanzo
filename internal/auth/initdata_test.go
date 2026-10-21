package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const fixtureToken = "123456:test-token"

// Fixed independently generated HMAC fixture; the expected signature is not
// computed by the implementation under test or the test signing helper.
const fixture = "auth_date=1792238400&query_id=AAE%2B%25%E4%B8%AD&user=%7B%22id%22%3A10001%2C%22first_name%22%3A%22%E6%B8%AC%E8%A9%A6+%2B+%25%22%7D&signature=abc%2B%2F%25%3D&hash=6b864debe63999cf4b5a31fcc614d2f1ee21e161d806a3982117bc3050a2a2e7"

func signed(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(fixtureToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

func TestVerifyFixedFixture(t *testing.T) {
	now := time.Unix(1792238400, 0)
	for _, raw := range []string{fixture, func() string {
		parts := strings.Split(fixture, "&")
		sort.Sort(sort.Reverse(sort.StringSlice(parts)))
		return strings.Join(parts, "&")
	}()} {
		id, issued, err := DecodeInitData(raw, fixtureToken, now)
		if err != nil || !Fresh(issued, now, 5*time.Minute) || id != 10001 {
			t.Fatalf("id=%d error=%v", id, err)
		}
	}
}

func TestRejectInvalidInitData(t *testing.T) {
	now := time.Unix(1792238400, 0)
	cases := map[string]string{
		"empty": "", "oversize": strings.Repeat("x", 16385),
		"tampered":  strings.Replace(fixture, "AAE", "BAE", 1),
		"duplicate": fixture + "&auth_date=1792238400", "bad_escape": fixture + "&x=%zz",
		"bad_hash": strings.Replace(fixture, "hash=6", "hash=z", 1),
	}
	for name, user := range map[string]string{"missing_user": "", "zero_id": "{\"id\":0}", "negative_id": "{\"id\":-1}", "fraction_id": "{\"id\":1.2}", "string_id": "{\"id\":\"1\"}", "overflow_id": "{\"id\":9223372036854775808}"} {
		v := url.Values{"auth_date": {"1792238400"}, "user": {user}}
		cases[name] = signed(v)
	}
	for name, date := range map[string]string{"expired": "1792238099", "future": "1792238431", "date_format": "+1792238400", "date_overflow": "9999999999999999999999"} {
		cases[name] = signed(url.Values{"auth_date": {date}, "user": {"{\"id\":10001}"}})
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, issued, err := DecodeInitData(raw, fixtureToken, now)
			switch {
			case err == ErrInvalidInitData:
			case err != nil:
				t.Fatalf("error=%v", err)
			case Fresh(issued, now, 5*time.Minute):
				t.Fatal("invalid init data accepted")
			}
		})
	}
	if _, _, err := DecodeInitData(fixture, "wrong", now); err == nil {
		t.Fatal("wrong token accepted")
	}
}

func TestAuthDateBoundaries(t *testing.T) {
	now := time.Unix(1792238400, 0)
	for _, date := range []string{"1792238100", "1792238430"} {
		raw := signed(url.Values{"auth_date": {date}, "user": {"{\"id\":10001}"}})
		_, issued, err := DecodeInitData(raw, fixtureToken, now)
		if err != nil || !Fresh(issued, now, 5*time.Minute) {
			t.Fatalf("error=%v issued=%v", err, issued)
		}
	}
}
