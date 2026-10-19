package service

import (
	"context"
	"database/sql"
	"strings"
	"telegram.bot.remind/internal/domain"
	"time"
)

const periodColumns = `p.id,p.service_id,p.period_type,p.name,p.starts_at,p.expires_at,p.date_precision,p.local_expires_date,p.timezone,p.status,p.cycle,p.created_at,p.updated_at,d.service_id,t.mode,t.interval_count,t.interval_unit,t.amount,t.currency,t.revision`
const periodJoins = ` FROM validity_periods p JOIN service_items s ON s.id=p.service_id LEFT JOIN sim_details d ON d.service_id=p.service_id LEFT JOIN period_terms t ON t.validity_period_id=p.id `

type periodRow struct {
	value    Period
	simID    sql.NullInt64
	mode     sql.NullString
	count    sql.NullInt64
	unit     sql.NullString
	amount   sql.NullInt64
	currency sql.NullString
	revision sql.NullInt64
}

func (r *periodRow) args() []any {
	return []any{&r.value.ID, &r.value.ServiceID, &r.value.PeriodType, &r.value.Name, &r.value.StartsAt, &r.value.ExpiresAt,
		&r.value.DatePrecision, &r.value.LocalExpiresDate, &r.value.Timezone, &r.value.Status, &r.value.Cycle,
		&r.value.CreatedAt, &r.value.UpdatedAt, &r.simID, &r.mode, &r.count, &r.unit, &r.amount, &r.currency, &r.revision}
}

func (r *periodRow) normalized() Period {
	if r.simID.Valid {
		id := r.simID.Int64
		r.value.SIMID = &id
	}
	r.value.Terms = PeriodTerms{Mode: "custom", Revision: 1}
	if r.mode.Valid {
		r.value.Terms.Mode = r.mode.String
		r.value.Terms.Revision = r.revision.Int64
		if r.count.Valid {
			n := r.count.Int64
			r.value.Terms.IntervalCount = &n
		}
		if r.unit.Valid {
			v := r.unit.String
			r.value.Terms.IntervalUnit = &v
		}
		if r.amount.Valid {
			n := r.amount.Int64
			r.value.Terms.Amount = &n
		}
		if r.currency.Valid {
			v := r.currency.String
			r.value.Terms.Currency = &v
		}
	}
	return r.value
}

func scanPeriod(row scanner) (Period, error) {
	var r periodRow
	if err := row.Scan(r.args()...); err != nil {
		return Period{}, err
	}
	return r.normalized(), nil
}

func (s *Service) getPeriod(ctx context.Context, q queryer, uid, id int64) (Period, error) {
	var r periodRow
	err := q.QueryRowContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.id=?", uid, id).Scan(r.args()...)
	if err != nil {
		return Period{}, dbError(err)
	}
	v := r.normalized()
	if v.Status != "cancelled" {
		v.Status = status(v.ExpiresAt, s.now().Unix())
	}
	return v, nil
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
	if !text(v.Name, 200, false) || (v.StartsAt != nil && *v.StartsAt >= v.ExpiresAt) {
		return false
	}
	if v.SIMID == nil && v.PeriodType != "custom" {
		return false
	}
	return v.PeriodType == "number_validity" || v.PeriodType == "plan_validity" || v.PeriodType == "data_package_validity" || v.PeriodType == "custom"
}
func validateRules(ctx context.Context, tx *sql.Tx, v Period) error {
	rows, e := tx.QueryContext(ctx, "SELECT days_before,reminder_time FROM reminder_rules WHERE validity_period_id=? AND enabled=1", v.ID)
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
	r, err := tx.ExecContext(ctx, `UPDATE period_terms SET mode=?,interval_count=?,interval_unit=?,amount=?,currency=?,revision=? WHERE validity_period_id=?`, t.Mode, t.IntervalCount, t.IntervalUnit, t.Amount, t.Currency, t.Revision, periodID)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil || n > 0 {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO period_terms(validity_period_id,mode,interval_count,interval_unit,amount,currency,revision) VALUES(?,?,?,?,?,?,?)`, periodID, t.Mode, t.IntervalCount, t.IntervalUnit, t.Amount, t.Currency, t.Revision)
	return err
}
func (s *Service) CreatePeriod(ctx context.Context, uid, serviceID int64, in PeriodInput) (Period, error) {
	v := Period{ServiceID: serviceID, PeriodType: in.PeriodType, Name: strings.TrimSpace(in.Name), Cycle: 1, CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix(), Terms: defaultTerms()}
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		parent, err := getServiceItem(ctx, tx, uid, serviceID)
		if err != nil {
			return err
		}
		if parent.Status != "active" {
			return ErrConflict
		}
		var terms *PeriodTerms
		if in.Terms != nil {
			t := termsInputValue(*in.Terms)
			if !validPeriodTerms(t) {
				return ErrInvalid
			}
			if !sameTerms(t, defaultTerms()) {
				terms = &t
			}
		}
		if parent.SIMDetails != nil {
			id := serviceID
			v.SIMID = &id
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
		v.Status = status(v.ExpiresAt, s.now().Unix())
		r, err := tx.ExecContext(ctx, `INSERT INTO validity_periods(service_id,period_type,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.ServiceID, v.PeriodType, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Status, v.Cycle, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			return err
		}
		v.ID, err = r.LastInsertId()
		if err != nil {
			return err
		}
		if terms == nil {
			return nil
		}
		v.Terms = *terms
		return savePeriodTerms(ctx, tx, v.ID, v.Terms)
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
	rows, err := s.store.DB.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.service_id=? AND p.id>? ORDER BY p.id LIMIT ?", uid, serviceID, after, limit+1)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanPeriod(rows)
		if err != nil {
			return out, ErrUnavailable
		}
		if v.Status != "cancelled" {
			v.Status = status(v.ExpiresAt, s.now().Unix())
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Period) int64 { return v.ID }), dbError(rows.Err())
}
func savePeriod(ctx context.Context, tx *sql.Tx, v Period) error {
	_, err := tx.ExecContext(ctx, `UPDATE validity_periods SET period_type=?,name=?,starts_at=?,expires_at=?,date_precision=?,local_expires_date=?,timezone=?,status=?,cycle=?,updated_at=? WHERE id=?`, v.PeriodType, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Status, v.Cycle, v.UpdatedAt, v.ID)
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
		if v.Status == "cancelled" || v.Cycle != in.ExpectedCycle {
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
		if in.PeriodType != nil {
			v.PeriodType = *in.PeriodType
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
func (s *Service) CancelPeriod(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, err := s.getPeriod(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		if v.Cycle != expected {
			return ErrConflict
		}
		if v.Status == "cancelled" {
			return nil
		}
		v.Status = "cancelled"
		v.UpdatedAt = s.now().Unix()
		if err = savePeriod(ctx, tx, v); err != nil {
			return err
		}
		return cancelJobs(ctx, tx, id, nil, v.UpdatedAt)
	})
}

// DeletePeriod permanently removes a period and its rules, jobs and history.
func (s *Service) DeletePeriod(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, err := s.getPeriod(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		if v.Cycle != expected {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM validity_periods WHERE id=? AND cycle=?", id, expected)
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
		rows, err := tx.QueryContext(ctx, "SELECT "+periodColumns+periodJoins+"WHERE s.user_id=? AND p.status!='cancelled'", uid)
		if err != nil {
			return err
		}
		var periods []Period
		for rows.Next() {
			v, e := scanPeriod(rows)
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
