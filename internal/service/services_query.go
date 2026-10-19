package service

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
)

// DueSoonDays is the shared calendar-day window for the service overview.
const DueSoonDays = 7

type OverviewCounts struct {
	Total    int
	DueSoon  int
	Expired  int
	NoPeriod int
}

// sortPeriods keeps the most urgent unexpired period first and falls back to
// the most recently expired one, matching the primary-period ordering.
func sortPeriods(periods []Period, now int64) {
	sort.SliceStable(periods, func(i, j int) bool {
		a, b := periods[i], periods[j]
		aActive, bActive := a.ExpiresAt > now, b.ExpiresAt > now
		if aActive != bActive {
			return aActive
		}
		if aActive {
			return a.ExpiresAt < b.ExpiresAt
		}
		return a.ExpiresAt > b.ExpiresAt
	})
}

// The primary period is the earliest unexpired period; when every period has
// expired, the most recently expired one is used. Equal cutoffs prefer the
// highest ID, so deleting a period never leaves a stale primary behind.
const primaryPeriodSQL = `(
    SELECT p2.id FROM service_periods p2
    WHERE p2.service_id = s.id
    ORDER BY (p2.expires_at <= ?),
             CASE WHEN p2.expires_at > ? THEN p2.expires_at ELSE -p2.expires_at END,
             p2.id DESC
    LIMIT 1
)`

// Service filters share the primary-period definition with the service
// overview, but never join sim_details as an ownership or resource-kind
// restriction.
func serviceFilterQuery(uid int64, filter ServiceFilter) (string, []any, string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if !text(filter.Query, 200, false) {
		return "", nil, "", ErrInvalid
	}
	if filter.Status == "" {
		filter.Status = "active"
	}
	where := " WHERE s.user_id=?"
	args := []any{uid}
	switch filter.Status {
	case "active", "due_soon", "expired", "no_period":
		where += " AND s.status='active'"
	case "archived":
		where += " AND s.status='archived'"
	case "all":
	default:
		return "", nil, "", ErrInvalid
	}
	if filter.CategoryID != nil {
		where += " AND s.category_id=?"
		args = append(args, *filter.CategoryID)
	}
	if filter.Query != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filter.Query) + "%"
		where += ` AND (s.name LIKE ? ESCAPE '\' OR s.provider LIKE ? ESCAPE '\' OR s.identifier LIKE ? ESCAPE '\')`
		args = append(args, pattern, pattern, pattern)
	}
	return where, args, filter.Status, nil
}

func (s *Service) serviceRead(ctx context.Context, uid int64, filter ServiceFilter) (*sql.Tx, *time.Location, error) {
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	fail := func(err error) (*sql.Tx, *time.Location, error) {
		tx.Rollback()
		return nil, nil, err
	}
	if err = s.authorize(ctx, tx, uid); err != nil {
		return fail(err)
	}
	if filter.CategoryID != nil {
		if _, err = categoryKind(ctx, tx, uid, *filter.CategoryID); err != nil {
			return fail(err)
		}
	}
	var zone string
	if err = tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&zone); err != nil {
		return fail(dbError(err))
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return fail(ErrUnavailable)
	}
	return tx, loc, nil
}

type primarySummary struct {
	id, expires          sql.NullInt64
	precision, localDate sql.NullString
}

func (p *primarySummary) args() []any {
	return []any{&p.id, &p.expires, &p.precision, &p.localDate}
}

type primaryState struct {
	noPeriod, expired, dueSoon bool
}

func (p primarySummary) state(now time.Time, loc *time.Location) (primaryState, error) {
	if !p.id.Valid {
		return primaryState{noPeriod: true}, nil
	}
	if !p.expires.Valid || !p.precision.Valid {
		return primaryState{}, ErrUnavailable
	}
	if p.expires.Int64 <= now.Unix() {
		return primaryState{expired: true}, nil
	}
	var date *string
	if p.localDate.Valid {
		date = &p.localDate.String
	}
	days, err := domain.CalendarDaysUntil(domain.Expiration{ExpiresAt: time.Unix(p.expires.Int64, 0).UTC(), DatePrecision: p.precision.String, LocalExpiresDate: date}, now, loc)
	if err != nil {
		return primaryState{}, ErrUnavailable
	}
	return primaryState{dueSoon: days >= 0 && days <= DueSoonDays}, nil
}

func (p primaryState) matches(filter string) bool {
	switch filter {
	case "due_soon":
		return p.dueSoon
	case "expired":
		return p.expired
	case "no_period":
		return p.noPeriod
	default:
		return true
	}
}

const servicePrimaryColumns = serviceColumns + `,p.id,p.expires_at,p.date_precision,p.local_expires_date`
const servicePrimaryJoins = ` FROM service_items s LEFT JOIN sim_details d ON d.service_id=s.id LEFT JOIN service_periods p ON p.id=` + primaryPeriodSQL

func (s *Service) ListServices(ctx context.Context, uid, after int64, limit int, filter ServiceFilter) (Page[ServiceItem], error) {
	out := Page[ServiceItem]{Data: []ServiceItem{}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	where, args, selectedStatus, err := serviceFilterQuery(uid, filter)
	if err != nil {
		return out, err
	}
	tx, loc, err := s.serviceRead(ctx, uid, filter)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	now := s.now()
	query := "SELECT " + servicePrimaryColumns + servicePrimaryJoins + where + " AND s.id>? ORDER BY s.id"
	args = append([]any{now.Unix(), now.Unix()}, args...)
	args = append(args, after)
	switch selectedStatus {
	case "due_soon", "expired", "no_period":
		// The primary period state is computed from calendar data, so SQL
		// cannot safely apply LIMIT before those rows are classified.
	default:
		query += " LIMIT ?"
		args = append(args, limit+1)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return out, dbError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sr serviceRow
		var p primarySummary
		if err = rows.Scan(append(sr.args(), p.args()...)...); err != nil {
			return out, dbError(err)
		}
		state, err := p.state(now, loc)
		if err != nil {
			return out, err
		}
		if !state.matches(selectedStatus) {
			continue
		}
		out.Data = append(out.Data, sr.value())
		if len(out.Data) > limit {
			break
		}
	}
	if err = rows.Err(); err != nil {
		return out, dbError(err)
	}
	return finishPage(out.Data, limit, func(v ServiceItem) int64 { return v.ID }), nil
}

func (s *Service) ServiceOverview(ctx context.Context, uid, after int64, limit int, filter ServiceFilter) (ServiceOverview, error) {
	out := ServiceOverview{Items: Page[ServiceOverviewItem]{Data: []ServiceOverviewItem{}}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	where, args, selectedStatus, err := serviceFilterQuery(uid, filter)
	if err != nil {
		return out, err
	}
	tx, loc, err := s.serviceRead(ctx, uid, filter)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	now := s.now()
	args = append([]any{now.Unix(), now.Unix()}, args...)
	rows, err := tx.QueryContext(ctx, "SELECT "+servicePrimaryColumns+servicePrimaryJoins+where+" ORDER BY s.id", args...)
	if err != nil {
		return out, dbError(err)
	}
	serviceIDs := make([]any, 0, limit+1)
	primaryIDs := make(map[int64]int64, limit+1)
	for rows.Next() {
		var sr serviceRow
		var p primarySummary
		if err = rows.Scan(append(sr.args(), p.args()...)...); err != nil {
			rows.Close()
			return out, dbError(err)
		}
		state, err := p.state(now, loc)
		if err != nil {
			rows.Close()
			return out, err
		}
		if !state.matches(selectedStatus) {
			continue
		}
		out.Counts.Total++
		switch {
		case state.noPeriod:
			out.Counts.NoPeriod++
		case state.expired:
			out.Counts.Expired++
		case state.dueSoon:
			out.Counts.DueSoon++
		}
		if sr.item.ID > after && len(out.Items.Data) < limit+1 {
			out.Items.Data = append(out.Items.Data, ServiceOverviewItem{Service: sr.value()})
			serviceIDs = append(serviceIDs, sr.item.ID)
			if p.id.Valid {
				primaryIDs[sr.item.ID] = p.id.Int64
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, dbError(err)
	}
	// Fetch every period for the selected rows in one batch after closing the
	// count cursor. Both queries use the same read snapshot and clock.
	if len(serviceIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(serviceIDs)), ",")
		periodArgs := append([]any{uid}, serviceIDs...)
		rows, err = tx.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.service_id IN ("+placeholders+") ORDER BY p.service_id,p.expires_at,p.id", periodArgs...)
		if err != nil {
			return out, dbError(err)
		}
		defer rows.Close()
		byService := make(map[int64]int, len(out.Items.Data))
		for i := range out.Items.Data {
			byService[out.Items.Data[i].Service.ID] = i
		}
		for rows.Next() {
			p, err := scanPeriod(rows, now.Unix())
			if err != nil {
				return out, dbError(err)
			}
			i, ok := byService[p.ServiceID]
			if !ok {
				return out, ErrUnavailable
			}
			if primaryIDs[p.ServiceID] == p.ID {
				value := p
				out.Items.Data[i].PrimaryPeriod = &value
			}
			out.Items.Data[i].Periods = append(out.Items.Data[i].Periods, p)
		}
		if err = rows.Err(); err != nil {
			return out, dbError(err)
		}
		for i := range out.Items.Data {
			sortPeriods(out.Items.Data[i].Periods, now.Unix())
		}
	}
	out.Items = finishPage(out.Items.Data, limit, func(v ServiceOverviewItem) int64 { return v.Service.ID })
	return out, nil
}
