package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

const (
	PrecisionDate     = "date"
	PrecisionDateTime = "datetime"
)

// ErrInvalidTime identifies invalid dates, timestamps, timezones and schedules.
var ErrInvalidTime = errors.New("invalid time")

// Expiration retains both the exclusive UTC cutoff and the user's date semantics.
// LocalExpiresDate is non-nil only for date precision. Timezone is an IANA name.
type Expiration struct {
	ExpiresAt        time.Time
	DatePrecision    string
	LocalExpiresDate *string
	Timezone         string
}

// wallClockPattern matches a local wall-clock timestamp without an offset.
// Minutes are required; seconds are optional and default to zero.
var wallClockPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}(:[0-9]{2})?$`)

var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$`)

// ParseExpiration parses a date or offset-qualified RFC3339 timestamp. A date is
// valid until the first instant of the next existing local date, exclusively.
// A completely skipped local date is not accepted.
func ParseExpiration(precision, value, timezone string) (Expiration, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" || timezone == "Local" {
		return Expiration{}, invalid("timezone must be an IANA timezone")
	}
	exp := Expiration{DatePrecision: precision, Timezone: timezone}
	switch precision {
	case PrecisionDate:
		date, err := parseDate(value)
		if err != nil {
			return Expiration{}, err
		}
		if _, ok := resolveWall(date, loc, false); !ok {
			return Expiration{}, invalid("date does not exist in timezone")
		}
		// A forward date-line transition can skip an entire following date.
		// Continue to the next existing local date instead of normalizing it
		// backwards with time.Date.
		for next := date.AddDate(0, 0, 1); ; next = next.AddDate(0, 0, 1) {
			if boundary, ok := resolveWall(next, loc, false); ok {
				exp.ExpiresAt = boundary
				break
			}
		}
		exp.LocalExpiresDate = &value
	case PrecisionDateTime:
		exp.ExpiresAt, err = parseTimestamp(value)
		if err != nil {
			return Expiration{}, err
		}
	default:
		return Expiration{}, invalid("date_precision must be date or datetime")
	}
	return exp, nil
}

// ParseStartsAt parses a supplied (nonempty) starts_at. Optional-field handling
// belongs to the caller; a supplied start must precede the exclusive cutoff.
func ParseStartsAt(value string, expiresAt time.Time) (time.Time, error) {
	start, err := parseTimestamp(value)
	if err != nil {
		return time.Time{}, err
	}
	if !start.Before(expiresAt) {
		return time.Time{}, invalid("starts_at must precede expires_at")
	}
	return start, nil
}

// ParseLocalDateTime resolves a wall-clock timestamp such as
// "2027-01-31 23:59" in an IANA timezone. A wall time skipped by a DST gap is
// rejected instead of silently moving; a wall time repeated by a DST overlap
// selects the earlier instant. Callers convert the result to RFC3339 with the
// zone's offset before handing it to the service layer.
func ParseLocalDateTime(value, timezone string) (time.Time, error) {
	if timezone == "" || timezone == "Local" || !wallClockPattern.MatchString(value) {
		return time.Time{}, invalid("local datetime must be YYYY-MM-DD HH:MM in an IANA timezone")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, invalid("timezone must be an IANA timezone")
	}
	normalized := value[:10] + "T" + value[11:]
	if len(normalized) == 16 {
		normalized += ":00"
	}
	wall, err := time.Parse("2006-01-02T15:04:05", normalized)
	if err != nil {
		return time.Time{}, invalid("local datetime must be an existing date and time")
	}
	// resolveWall moves a skipped wall time forward. Requested cutoffs must not
	// silently move, so the resolved fields must match the input exactly.
	at, ok := resolveWall(wall, loc, false)
	if !ok {
		return time.Time{}, invalid("local datetime does not exist in timezone")
	}
	actual := at.In(loc)
	if actual.Year() != wall.Year() || actual.Month() != wall.Month() || actual.Day() != wall.Day() ||
		actual.Hour() != wall.Hour() || actual.Minute() != wall.Minute() || actual.Second() != wall.Second() {
		return time.Time{}, invalid("local datetime does not exist in timezone")
	}
	return at.UTC(), nil
}

// ReminderAt subtracts calendar days, then resolves the requested local HH:MM.
// A gap moves forward to the first valid local minute on that date; an overlap
// selects the earliest UTC occurrence. Reminders must precede the cutoff.
func ReminderAt(exp Expiration, daysBefore int64, reminderTime string) (time.Time, error) {
	if daysBefore < 0 {
		return time.Time{}, invalid("days_before must be nonnegative")
	}
	// Valid four-digit expiration dates cannot produce an earlier supported
	// calendar date with a displacement exceeding this many days. This also
	// prevents integer overflow in AddDate on all platforms.
	if daysBefore > 366*10000 {
		return time.Time{}, invalid("days_before is outside the supported calendar")
	}
	if len(reminderTime) != 5 || reminderTime[2] != ':' {
		return time.Time{}, invalid("reminder_time must be HH:MM")
	}
	clock, err := time.Parse("15:04", reminderTime)
	if err != nil || clock.Format("15:04") != reminderTime {
		return time.Time{}, invalid("reminder_time must be HH:MM")
	}
	loc, err := time.LoadLocation(exp.Timezone)
	if err != nil || exp.Timezone == "" || exp.Timezone == "Local" {
		return time.Time{}, invalid("timezone must be an IANA timezone")
	}
	var date time.Time
	switch exp.DatePrecision {
	case PrecisionDate:
		if exp.LocalExpiresDate == nil {
			return time.Time{}, invalid("date precision requires local_expires_date")
		}
		date, err = parseDate(*exp.LocalExpiresDate)
		if err != nil {
			return time.Time{}, err
		}
	case PrecisionDateTime:
		if exp.LocalExpiresDate != nil {
			return time.Time{}, invalid("datetime must not have local_expires_date")
		}
		local := exp.ExpiresAt.In(loc)
		date = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return time.Time{}, invalid("date_precision must be date or datetime")
	}
	date = date.AddDate(0, 0, -int(daysBefore))
	if date.Year() < 0 || date.Year() > 9999 {
		return time.Time{}, invalid("reminder date is outside the supported calendar")
	}
	wall := date.Add(time.Duration(clock.Hour())*time.Hour + time.Duration(clock.Minute())*time.Minute)
	at, ok := resolveWall(wall, loc, true)
	if !ok {
		return time.Time{}, invalid("reminder date does not exist in timezone")
	}
	if !at.Before(exp.ExpiresAt) {
		return time.Time{}, invalid("reminder must precede expires_at; choose an earlier time or more days_before")
	}
	return at, nil
}

// CalendarDaysUntil counts Gregorian calendar dates in loc, not elapsed 24-hour
// intervals. Date precision uses the stored local date; datetime uses the local
// date of its instant. The caller resolves loc once from the user's IANA zone.
func CalendarDaysUntil(exp Expiration, now time.Time, loc *time.Location) (int, error) {
	if loc == nil {
		return 0, invalid("timezone is required")
	}
	var target time.Time
	var err error
	switch exp.DatePrecision {
	case PrecisionDate:
		if exp.LocalExpiresDate == nil {
			return 0, invalid("date precision requires a local date")
		}
		target, err = parseDate(*exp.LocalExpiresDate)
		if err != nil {
			return 0, err
		}
	case PrecisionDateTime:
		at := exp.ExpiresAt.In(loc)
		target = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return 0, invalid("date_precision must be date or datetime")
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	// UTC date labels, not local instants: this counts calendar dates across
	// DST and stays exact beyond time.Duration's approximately 290-year range.
	return int((target.Unix() - today.Unix()) / 86400), nil
}

func parseDate(value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || len(value) != 10 || date.Format("2006-01-02") != value {
		return time.Time{}, invalid("date must be an existing YYYY-MM-DD")
	}
	return date, nil
}

func parseTimestamp(value string) (time.Time, error) {
	if !timestampPattern.MatchString(value) {
		return time.Time{}, invalid("datetime must be offset-qualified RFC3339")
	}
	// time.Parse historically accepts offset hours 24 and offset minutes 60;
	// RFC3339 does not. Validate these before invoking it.
	if value[len(value)-1] != 'Z' {
		offset := value[len(value)-5:]
		if offset[:2] > "23" || offset[3:] > "59" {
			return time.Time{}, invalid("datetime has an invalid UTC offset")
		}
	}
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, invalid("datetime must be offset-qualified RFC3339")
	}
	return at.UTC(), nil
}

// resolveWall treats a UTC time's fields as local wall-clock fields. It walks
// actual zone intervals rather than trusting time.Date's arbitrary choice in
// overlaps and gaps. IANA offsets and date-line jumps fit within the 72-hour
// search margin. The selected wall time never crosses into another local date.
func resolveWall(wall time.Time, loc *time.Location, minute bool) (time.Time, bool) {
	endDay := time.Date(wall.Year(), wall.Month(), wall.Day()+1, 0, 0, 0, 0, time.UTC)
	start, end := wall.Add(-72*time.Hour), wall.Add(72*time.Hour)
	var best, bestWall time.Time
	found := false
	for cursor := start; cursor.Before(end); {
		local := cursor.In(loc)
		_, offset := local.Zone()
		lower, upper := local.ZoneBounds()
		if lower.IsZero() || lower.Before(start) {
			lower = start
		}
		if upper.IsZero() || upper.After(end) {
			upper = end
		}
		candidateWall := wall
		localLower := lower.UTC().Add(time.Duration(offset) * time.Second)
		if localLower.After(candidateWall) {
			candidateWall = localLower
		}
		if minute && (candidateWall.Second() != 0 || candidateWall.Nanosecond() != 0) {
			candidateWall = candidateWall.Truncate(time.Minute).Add(time.Minute)
		}
		candidate := candidateWall.Add(-time.Duration(offset) * time.Second)
		if candidateWall.Before(endDay) && !candidate.Before(lower) && candidate.Before(upper) {
			// Verify local fields explicitly: an interval must actually map
			// to the desired civil time, including historical second offsets.
			actual := candidate.In(loc)
			if actual.Year() == candidateWall.Year() && actual.Month() == candidateWall.Month() && actual.Day() == candidateWall.Day() && actual.Hour() == candidateWall.Hour() && actual.Minute() == candidateWall.Minute() && actual.Second() == candidateWall.Second() && (!found || candidateWall.Before(bestWall) || (candidateWall.Equal(bestWall) && candidate.Before(best))) {
				best, bestWall, found = candidate.UTC(), candidateWall, true
			}
		}
		cursor = upper
	}
	return best, found
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidTime, message)
}
