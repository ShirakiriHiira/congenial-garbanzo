package service

import (
	"context"
	"database/sql"
	"strings"
	"telegram.bot.remind/internal/domain"
	"time"
)

const periodColumns = `p.id,p.sim_id,p.period_type,p.name,p.starts_at,p.expires_at,p.date_precision,p.local_expires_date,p.timezone,p.status,p.cycle,p.created_at,p.updated_at`

func scanPeriod(row scanner) (Period, error) {
	var v Period
	e := row.Scan(&v.ID, &v.SIMID, &v.PeriodType, &v.Name, &v.StartsAt, &v.ExpiresAt, &v.DatePrecision, &v.LocalExpiresDate, &v.Timezone, &v.Status, &v.Cycle, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func (s *Service) getPeriod(ctx context.Context, q queryer, uid, id int64) (Period, error) {
	v, e := scanPeriod(q.QueryRowContext(ctx, "SELECT "+periodColumns+" FROM validity_periods p JOIN sims s ON s.id=p.sim_id WHERE s.user_id=? AND p.id=?", uid, id))
	if e == nil && v.Status != "cancelled" {
		v.Status = status(v.ExpiresAt, s.now().Unix())
	}
	return v, dbError(e)
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
	return text(v.Name, 200, false) && (v.PeriodType == "number_validity" || v.PeriodType == "plan_validity" || v.PeriodType == "data_package_validity" || v.PeriodType == "custom") && (v.StartsAt == nil || *v.StartsAt < v.ExpiresAt)
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
func (s *Service) CreatePeriod(ctx context.Context, uid, simID int64, in PeriodInput) (Period, error) {
	v := Period{SIMID: simID, PeriodType: in.PeriodType, Name: strings.TrimSpace(in.Name), Cycle: 1, CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix()}
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		sim, e := getSIM(ctx, tx, uid, simID)
		if e != nil {
			return e
		}
		if sim.Status != "active" {
			return ErrConflict
		}
		if e = tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&v.Timezone); e != nil {
			return e
		}
		if e = applyExpiration(&v, in.DatePrecision, in.ExpiresAt); e != nil {
			return e
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
		r, e := tx.ExecContext(ctx, `INSERT INTO validity_periods(sim_id,period_type,name,starts_at,expires_at,date_precision,local_expires_date,timezone,status,cycle,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.SIMID, v.PeriodType, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Status, v.Cycle, v.CreatedAt, v.UpdatedAt)
		if e == nil {
			v.ID, e = r.LastInsertId()
		}
		return e
	})
	return v, e
}
func (s *Service) GetPeriod(ctx context.Context, uid, id int64) (Period, error) {
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return Period{}, e
	}
	return s.getPeriod(ctx, s.store.DB, uid, id)
}
func (s *Service) ListPeriods(ctx context.Context, uid, simID, after int64, limit int) (Page[Period], error) {
	out := Page[Period]{Data: []Period{}}
	if e := pageArgs(after, limit); e != nil {
		return out, e
	}
	if _, e := s.GetSIM(ctx, uid, simID); e != nil {
		return out, e
	}
	rows, e := s.store.DB.QueryContext(ctx, "SELECT "+periodColumns+" FROM validity_periods p JOIN sims s ON s.id=p.sim_id WHERE s.user_id=? AND p.sim_id=? AND p.id>? ORDER BY p.id LIMIT ?", uid, simID, after, limit+1)
	if e != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanPeriod(rows)
		if e != nil {
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
	_, e := tx.ExecContext(ctx, `UPDATE validity_periods SET period_type=?,name=?,starts_at=?,expires_at=?,date_precision=?,local_expires_date=?,timezone=?,status=?,cycle=?,updated_at=? WHERE id=?`, v.PeriodType, v.Name, v.StartsAt, v.ExpiresAt, v.DatePrecision, v.LocalExpiresDate, v.Timezone, v.Status, v.Cycle, v.UpdatedAt, v.ID)
	return e
}
func (s *Service) UpdatePeriod(ctx context.Context, uid, id int64, in PeriodPatch) (Period, error) {
	var v Period
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var e error
		v, e = s.getPeriod(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Status == "cancelled" || v.Cycle != in.ExpectedCycle {
			return ErrConflict
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
			if e = applyExpiration(&v, precision, value); e != nil {
				return e
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
			if e = validateRules(ctx, tx, v); e != nil {
				return e
			}
			v.Cycle++
			if e = cancelJobs(ctx, tx, id, nil, s.now().Unix()); e != nil {
				return e
			}
		}
		v.Status = status(v.ExpiresAt, s.now().Unix())
		v.UpdatedAt = s.now().Unix()
		return savePeriod(ctx, tx, v)
	})
	return v, e
}
func (s *Service) CancelPeriod(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, e := s.getPeriod(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Cycle != expected {
			return ErrConflict
		}
		if v.Status == "cancelled" {
			return nil
		}
		v.Status = "cancelled"
		v.UpdatedAt = s.now().Unix()
		if e = savePeriod(ctx, tx, v); e != nil {
			return e
		}
		return cancelJobs(ctx, tx, id, nil, v.UpdatedAt)
	})
}

// DeletePeriod permanently removes a period and its rules, jobs and history.
func (s *Service) DeletePeriod(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, e := s.getPeriod(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Cycle != expected {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM validity_periods WHERE id=? AND cycle=?", id, expected)
		return e
	})
}
func (s *Service) SetTimezone(ctx context.Context, uid int64, zone string) (User, error) {
	if zone == "" || zone == "Local" {
		return User{}, ErrInvalid
	}
	if _, e := time.LoadLocation(zone); e != nil {
		return User{}, ErrInvalid
	}
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var old string
		if e := tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&old); e != nil {
			return e
		}
		if old == zone {
			return nil
		}
		rows, e := tx.QueryContext(ctx, "SELECT "+periodColumns+" FROM validity_periods p JOIN sims s ON s.id=p.sim_id WHERE s.user_id=? AND p.status!='cancelled'", uid)
		if e != nil {
			return e
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
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		now := s.now().Unix()
		for _, v := range periods {
			v.Timezone = zone
			if v.DatePrecision == "date" {
				if e = applyExpiration(&v, "date", *v.LocalExpiresDate); e != nil {
					return e
				}
			}
			if !periodValid(v) {
				return ErrInvalid
			}
			if e = validateRules(ctx, tx, v); e != nil {
				return e
			}
			v.Cycle++
			v.UpdatedAt = now
			v.Status = status(v.ExpiresAt, now)
			if e = savePeriod(ctx, tx, v); e != nil {
				return e
			}
			if e = cancelJobs(ctx, tx, v.ID, nil, now); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, "UPDATE users SET timezone=?,updated_at=? WHERE id=?", zone, now, uid)
		return e
	})
	if e != nil {
		return User{}, e
	}
	return s.Me(ctx, uid)
}
