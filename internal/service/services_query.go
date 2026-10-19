package service

import (
	"context"
	"database/sql"
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

const primaryPeriodSQL = `(
    SELECT p2.id FROM validity_periods p2
    WHERE p2.service_id = s.id AND p2.status != 'cancelled'
    ORDER BY p2.expires_at, p2.id DESC LIMIT 1
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
const servicePrimaryJoins = ` FROM service_items s LEFT JOIN sim_details d ON d.service_id=s.id LEFT JOIN validity_periods p ON p.id=` + primaryPeriodSQL

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
	rows, err := tx.QueryContext(ctx, "SELECT "+servicePrimaryColumns+servicePrimaryJoins+where+" ORDER BY s.id", args...)
	if err != nil {
		return out, dbError(err)
	}
	periodIDs := make([]any, 0, limit+1)
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
			if p.id.Valid {
				periodIDs = append(periodIDs, p.id.Int64)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, dbError(err)
	}
	// Fetch the selected periods in one batch after closing the count cursor.
	// Both queries use the same read snapshot and the same captured clock.
	if len(periodIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(periodIDs)), ",")
		periodArgs := append([]any{uid}, periodIDs...)
		rows, err = tx.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.id IN ("+placeholders+")", periodArgs...)
		if err != nil {
			return out, dbError(err)
		}
		defer rows.Close()
		byService := make(map[int64]int, len(out.Items.Data))
		for i := range out.Items.Data {
			byService[out.Items.Data[i].Service.ID] = i
		}
		for rows.Next() {
			p, err := scanPeriod(rows)
			if err != nil {
				return out, dbError(err)
			}
			p.Status = status(p.ExpiresAt, now.Unix())
			i, ok := byService[p.ServiceID]
			if !ok {
				return out, ErrUnavailable
			}
			out.Items.Data[i].PrimaryPeriod = &p
		}
		if err = rows.Err(); err != nil {
			return out, dbError(err)
		}
	}
	out.Items = finishPage(out.Items.Data, limit, func(v ServiceOverviewItem) int64 { return v.Service.ID })
	return out, nil
}
