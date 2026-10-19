package service

import (
	"context"
	"database/sql"
	"telegram.bot.remind/internal/domain"
)

const renewalColumns = `n.id,n.period_id,n.previous_cycle,n.previous_expires_at,n.new_expires_at,n.previous_date_precision,n.new_date_precision,n.previous_local_expires_date,n.new_local_expires_date,n.timezone,n.renewed_at,n.amount,n.currency,n.notes,n.created_at`

func scanRenewal(row scanner) (Renewal, error) {
	var v Renewal
	e := row.Scan(&v.ID, &v.PeriodID, &v.PreviousCycle, &v.PreviousExpiresAt, &v.NewExpiresAt, &v.PreviousDatePrecision, &v.NewDatePrecision, &v.PreviousLocalExpiresDate, &v.NewLocalExpiresDate, &v.Timezone, &v.RenewedAt, &v.Amount, &v.Currency, &v.Notes, &v.CreatedAt)
	return v, e
}
func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func validMoney(in RenewInput) bool {
	if !optionalText(in.Notes, 4000) || (in.Amount == nil) != (in.Currency == nil) {
		return false
	}
	if in.Amount == nil {
		return true
	}
	if *in.Amount < 0 || len(*in.Currency) != 3 {
		return false
	}
	for _, c := range *in.Currency {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}
func (s *Service) renew(ctx context.Context, tx *sql.Tx, uid, pid int64, in RenewInput) (Renewal, error) {
	var out Renewal
	if in.ExpectedCycle < 1 || !validMoney(in) {
		return out, ErrInvalid
	}
	p, e := s.getPeriod(ctx, tx, uid, pid)
	if e != nil {
		return out, e
	}
	existing, e := scanRenewal(tx.QueryRowContext(ctx, "SELECT "+renewalColumns+" FROM renewals n JOIN service_periods p ON p.id=n.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND n.period_id=? AND n.previous_cycle=?", uid, pid, in.ExpectedCycle))
	if e == nil {
		ex, err := domain.ParseExpiration(in.DatePrecision, in.ExpiresAt, existing.Timezone)
		if err != nil {
			return out, ErrInvalid
		}
		if ex.ExpiresAt.Unix() != existing.NewExpiresAt || ex.DatePrecision != existing.NewDatePrecision || !equalPtr(ex.LocalExpiresDate, existing.NewLocalExpiresDate) || !equalPtr(in.Amount, existing.Amount) || !equalPtr(in.Currency, existing.Currency) || !equalPtr(in.Notes, existing.Notes) {
			return out, ErrConflict
		}
		existing.Replayed = true
		return existing, nil
	}
	if e != sql.ErrNoRows {
		return out, e
	}
	if p.Cycle != in.ExpectedCycle {
		return out, ErrConflict
	}
	old := p
	if e = applyExpiration(&p, in.DatePrecision, in.ExpiresAt); e != nil {
		return out, e
	}
	now := s.now().Unix()
	if p.ExpiresAt <= old.ExpiresAt || p.ExpiresAt <= now || !periodValid(p) {
		return out, ErrInvalid
	}
	if e = validateRules(ctx, tx, p); e != nil {
		return out, e
	}
	out = Renewal{PeriodID: pid, PreviousCycle: old.Cycle, PreviousExpiresAt: old.ExpiresAt, NewExpiresAt: p.ExpiresAt, PreviousDatePrecision: old.DatePrecision, NewDatePrecision: p.DatePrecision, PreviousLocalExpiresDate: old.LocalExpiresDate, NewLocalExpiresDate: p.LocalExpiresDate, Timezone: p.Timezone, RenewedAt: now, Amount: in.Amount, Currency: in.Currency, Notes: in.Notes, CreatedAt: now}
	r, e := tx.ExecContext(ctx, `INSERT INTO renewals(period_id,previous_cycle,previous_expires_at,new_expires_at,previous_date_precision,new_date_precision,previous_local_expires_date,new_local_expires_date,timezone,renewed_at,amount,currency,notes,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, pid, out.PreviousCycle, out.PreviousExpiresAt, out.NewExpiresAt, out.PreviousDatePrecision, out.NewDatePrecision, out.PreviousLocalExpiresDate, out.NewLocalExpiresDate, out.Timezone, now, out.Amount, out.Currency, out.Notes, now)
	if e != nil {
		return out, e
	}
	out.ID, e = r.LastInsertId()
	if e != nil {
		return out, e
	}
	p.Cycle++
	p.Status = status(p.ExpiresAt, now)
	p.UpdatedAt = now
	if e = savePeriod(ctx, tx, p); e != nil {
		return out, e
	}
	return out, cancelJobs(ctx, tx, pid, nil, now)
}
func (s *Service) Renew(ctx context.Context, uid, pid int64, in RenewInput) (Renewal, error) {
	var out Renewal
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error { var e error; out, e = s.renew(ctx, tx, uid, pid, in); return e })
	return out, e
}
func (s *Service) ListRenewals(ctx context.Context, uid, pid, after int64, limit int) (Page[Renewal], error) {
	out := Page[Renewal]{Data: []Renewal{}}
	if e := pageArgs(after, limit); e != nil {
		return out, e
	}
	if _, e := s.GetPeriod(ctx, uid, pid); e != nil {
		return out, e
	}
	rows, e := s.store.DB.QueryContext(ctx, "SELECT "+renewalColumns+" FROM renewals n JOIN service_periods p ON p.id=n.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND p.id=? AND n.id>? ORDER BY n.id LIMIT ?", uid, pid, after, limit+1)
	if e != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanRenewal(rows)
		if e != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Renewal) int64 { return v.ID }), dbError(rows.Err())
}
