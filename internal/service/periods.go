package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"telegram.bot.remind/internal/domain"
)

const periodColumns = `p.id,p.service_id,p.name,p.starts_at,p.expires_at,p.date_precision,p.local_expires_date,p.timezone,p.cycle,p.created_at,p.updated_at,t.mode,t.interval_count,t.interval_unit,t.amount,t.currency,t.revision`
const periodJoins = ` FROM service_periods p JOIN service_items s ON s.id=p.service_id JOIN period_terms t ON t.period_id=p.id `

type periodRow struct {
	value Period
}

func (r *periodRow) args() []any {
	return []any{&r.value.ID, &r.value.ServiceID, &r.value.Name, &r.value.StartsAt, &r.value.ExpiresAt,
		&r.value.DatePrecision, &r.value.LocalExpiresDate, &r.value.Timezone, &r.value.Cycle,
		&r.value.CreatedAt, &r.value.UpdatedAt, &r.value.Terms.Mode, &r.value.Terms.IntervalCount,
		&r.value.Terms.IntervalUnit, &r.value.Terms.Amount, &r.value.Terms.Currency, &r.value.Terms.Revision}
}

// normalized derives the only status source from the current clock. There is
// no persisted status column and no cancelled state.
func (r *periodRow) normalized(now int64) Period {
	r.value.Status = status(r.value.ExpiresAt, now)
	return r.value
}

func scanPeriod(row scanner, now int64) (Period, error) {
	var r periodRow
	if err := row.Scan(r.args()...); err != nil {
		return Period{}, err
	}
	return r.normalized(now), nil
}

func (s *Service) getPeriod(ctx context.Context, q queryer, uid, id int64) (Period, error) {
	var r periodRow
	err := q.QueryRowContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.id=?", uid, id).Scan(r.args()...)
	if err != nil {
		return Period{}, dbError(err)
	}
	return r.normalized(s.now().Unix()), nil
}

func expiration(v Period) domain.Expiration {
	return domain.Expiration{ExpiresAt: time.Unix(v.ExpiresAt, 0).UTC(), DatePrecision: v.DatePrecision, LocalExpiresDate: v.LocalExpiresDate, Timezone: v.Timezone}
}
func applyExpiration(v *Period, precision, value string) error {
	ex, e := domain.ParseExpiration(precision, value, v.Timezone)
	if e != nil {
		return ErrInvalid
	}
	v.ExpiresAt = ex.ExpiresAt.Unix()
	v.DatePrecision = ex.DatePrecision
	v.LocalExpiresDate = ex.LocalExpiresDate
	return nil
}
func periodValid(v Period) bool {
	return text(v.Name, 200, false) && (v.StartsAt == nil || *v.StartsAt < v.ExpiresAt)
}
func validateRules(ctx context.Context, tx *sql.Tx, v Period) error {
	rows, e := tx.QueryContext(ctx, "SELECT days_before,reminder_time FROM reminder_rules WHERE period_id=? AND enabled=1", v.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var d int64
		var t string
		if e = rows.Scan(&d, &t); e != nil {
			return e
		}
		if _, e = domain.ReminderAt(expiration(v), d, t); e != nil {
			return ErrInvalid
		}
	}
	return rows.Err()
}

func defaultTerms() PeriodTerms { return PeriodTerms{Mode: "custom", Revision: 1} }

func validPeriodTerms(t PeriodTerms) bool {
	switch t.Mode {
	case "once", "custom":
		if t.IntervalCount != nil || t.IntervalUnit != nil {
			return false
		}
	case "recurring":
		if t.IntervalCount == nil || *t.IntervalCount < 1 || t.IntervalUnit == nil {
			return false
		}
		switch *t.IntervalUnit {
		case "day", "week", "month", "year":
		default:
			return false
		}
	default:
		return false
	}
	if (t.Amount == nil) != (t.Currency == nil) {
		return false
	}
	if t.Amount != nil {
		if *t.Amount < 0 || len(*t.Currency) != 3 {
			return false
		}
		for _, c := range *t.Currency {
			if c < 'A' || c > 'Z' {
				return false
			}
		}
	}
	return true
}

func termsInputValue(in TermsInput) PeriodTerms {
	return PeriodTerms{Mode: in.Mode, IntervalCount: in.IntervalCount, IntervalUnit: in.IntervalUnit, Amount: in.Amount, Currency: in.Currency, Revision: 1}
}

func sameTerms(a, b PeriodTerms) bool {
	return a.Mode == b.Mode && equalPtr(a.IntervalCount, b.IntervalCount) && equalPtr(a.IntervalUnit, b.IntervalUnit) &&
		equalPtr(a.Amount, b.Amount) && equalPtr(a.Currency, b.Currency)
}

func savePeriodTerms(ctx context.Context, tx *sql.Tx, periodID int64, t PeriodTerms) error {
	r, err := tx.ExecContext(ctx, `UPDATE period_terms SET mode=?,interval_count=?,interval_unit=?,amount=?,currency=?,revision=? WHERE period_id=?`, t.Mode, t.IntervalCount, t.IntervalUnit, t.Amount, t.Currency, t.Revision, periodID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil || n > 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO period_terms(period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(?,?,?,?,?,?,?)`, periodID, t.Mode, t.IntervalCount, t.IntervalUnit, t.Amount, t.Currency, t.Revision)
	return err
}
func (s *Service) CreatePeriod(ctx context.Context, uid, serviceID int64, in PeriodInput) (Period, error) {
	now := s.now().Unix()
	v := Period{ServiceID: serviceID, Name: strings.TrimSpace(in.Name), Cycle: 1, CreatedAt: now, UpdatedAt: now, Terms: defaultTerms()}
	if in.Terms != nil {
		v.Terms = termsInputValue(*in.Terms)
	}
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		parent, err := getServiceItem(ctx, tx, uid, serviceID)
		if err != nil {
			return err
		}
		if parent.Status != "active" {
			return ErrConflict
		}
		if !validPeriodTerms(v.Terms) {
			return ErrInvalid
		}
		if err = tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&v.Timezone); err != nil {
			return err
		}
		if err = applyExpiration(&v, in.DatePrecision, in.ExpiresAt); err != nil {
			return err
		}
		if in.StartsAt != nil {
			t, e := domain.ParseStartsAt(*in.StartsAt, time.Unix(v.ExpiresAt, 0))
			if e != nil {
				return ErrInvalid
			}
			n := t.Unix()
			v.StartsAt = &n
		}
		if !periodValid(v) {
			return ErrInvalid
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO service_periods(service_id,name,starts_at,expires_at,date_precision,local_expires_date,timezone,cycle,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.ServiceID, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Cycle, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			return err
		}
		v.ID, err = r.LastInsertId()
		if err != nil {
			return err
		}
		if err = savePeriodTerms(ctx, tx, v.ID, v.Terms); err != nil {
			return err
		}
		v.Status = status(v.ExpiresAt, now)
		return nil
	})
	return v, err
}
func (s *Service) GetPeriod(ctx context.Context, uid, id int64) (Period, error) {
	if err := s.authorize(ctx, s.store.DB, uid); err != nil {
		return Period{}, err
	}
	return s.getPeriod(ctx, s.store.DB, uid, id)
}
func (s *Service) ListPeriods(ctx context.Context, uid, serviceID, after int64, limit int) (Page[Period], error) {
	out := Page[Period]{Data: []Period{}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	if _, err := s.GetService(ctx, uid, serviceID); err != nil {
		return out, err
	}
	now := s.now().Unix()
	rows, err := s.store.DB.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.service_id=? AND p.id>? ORDER BY p.id LIMIT ?", uid, serviceID, after, limit+1)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanPeriod(rows, now)
		if err != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Period) int64 { return v.ID }), dbError(rows.Err())
}
func savePeriod(ctx context.Context, tx *sql.Tx, v Period) error {
	_, err := tx.ExecContext(ctx, `UPDATE service_periods SET name=?,starts_at=?,expires_at=?,date_precision=?,local_expires_date=?,timezone=?,cycle=?,updated_at=? WHERE id=?`, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Cycle, v.UpdatedAt, v.ID)
	return err
}
func (s *Service) UpdatePeriod(ctx context.Context, uid, id int64, in PeriodPatch) (Period, error) {
	var v Period
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var err error
		v, err = s.getPeriod(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		if v.Cycle != in.ExpectedCycle {
			return ErrConflict
		}
		if in.Terms.Set != (in.ExpectedTermsRevision != nil) ||
			(in.ExpectedTermsRevision != nil && *in.ExpectedTermsRevision < 1) {
			return ErrInvalid
		}
		if in.ExpectedTermsRevision != nil && v.Terms.Revision != *in.ExpectedTermsRevision {
			return ErrConflict
		}
		nextTerms := v.Terms
		if in.Terms.Set {
			if in.Terms.Value == nil {
				nextTerms = defaultTerms()
			} else {
				nextTerms = termsInputValue(*in.Terms.Value)
				if !validPeriodTerms(nextTerms) {
					return ErrInvalid
				}
			}
		}
		if in.Name != nil {
			v.Name = strings.TrimSpace(*in.Name)
		}
		beforeExpiresAt, beforePrecision := v.ExpiresAt, v.DatePrecision
		changed := in.ExpiresAt != nil || in.DatePrecision != nil
		if changed {
			precision := v.DatePrecision
			if in.DatePrecision != nil {
				precision = *in.DatePrecision
			}
			value := time.Unix(v.ExpiresAt, 0).UTC().Format(time.RFC3339)
			if v.LocalExpiresDate != nil {
				value = *v.LocalExpiresDate
			}
			if in.ExpiresAt != nil {
				value = *in.ExpiresAt
			}
			if err = applyExpiration(&v, precision, value); err != nil {
				return err
			}
		}
		if in.StartsAt.Set {
			v.StartsAt = nil
			if in.StartsAt.Value != nil {
				t, e := domain.ParseStartsAt(*in.StartsAt.Value, time.Unix(v.ExpiresAt, 0))
				if e != nil {
					return ErrInvalid
				}
				n := t.Unix()
				v.StartsAt = &n
			}
		}
		expirationChanged := v.ExpiresAt != beforeExpiresAt || v.DatePrecision != beforePrecision
		if !periodValid(v) {
			return ErrInvalid
		}
		if expirationChanged {
			if err = validateRules(ctx, tx, v); err != nil {
				return err
			}
			v.Cycle++
			if err = cancelJobs(ctx, tx, id, nil, s.now().Unix()); err != nil {
				return err
			}
		}
		if in.Terms.Set && !sameTerms(v.Terms, nextTerms) {
			nextTerms.Revision = v.Terms.Revision + 1
			if err = savePeriodTerms(ctx, tx, id, nextTerms); err != nil {
				return err
			}
			v.Terms = nextTerms
		}
		v.Status = status(v.ExpiresAt, s.now().Unix())
		v.UpdatedAt = s.now().Unix()
		return savePeriod(ctx, tx, v)
	})
	return v, err
}

// DeletePeriod permanently removes a period and its terms, rules, jobs and
// renewal history through foreign-key cascades.
func (s *Service) DeletePeriod(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, err := s.getPeriod(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		if v.Cycle != expected {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM service_periods WHERE id=? AND cycle=?", id, expected)
		return err
	})
}
func (s *Service) SetTimezone(ctx context.Context, uid int64, zone string) (User, error) {
	if zone == "" || zone == "Local" {
		return User{}, ErrInvalid
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return User{}, ErrInvalid
	}
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var old string
		if err := tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&old); err != nil {
			return err
		}
		if old == zone {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=?", uid)
		if err != nil {
			return err
		}
		var periods []Period
		for rows.Next() {
			v, e := scanPeriod(rows, s.now().Unix())
			if e != nil {
				rows.Close()
				return e
			}
			periods = append(periods, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		now := s.now().Unix()
		for _, v := range periods {
			v.Timezone = zone
			if v.DatePrecision == "date" {
				if err = applyExpiration(&v, "date", *v.LocalExpiresDate); err != nil {
					return err
				}
			}
			if !periodValid(v) {
				return ErrInvalid
			}
			if err = validateRules(ctx, tx, v); err != nil {
				return err
			}
			v.Cycle++
			v.UpdatedAt = now
			v.Status = status(v.ExpiresAt, now)
			if err = savePeriod(ctx, tx, v); err != nil {
				return err
			}
			if err = cancelJobs(ctx, tx, v.ID, nil, now); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE users SET timezone=?,updated_at=? WHERE id=?", zone, now, uid)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return s.Me(ctx, uid)
}
