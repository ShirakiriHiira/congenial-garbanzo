package service

import (
	"context"
	"database/sql"
	"telegram.bot.remind/internal/domain"
)

const ruleColumns = `r.id,r.period_id,r.days_before,r.reminder_time,r.enabled,r.revision,r.created_at,r.updated_at`

func scanRule(row scanner) (Rule, error) {
	var v Rule
	e := row.Scan(&v.ID, &v.PeriodID, &v.DaysBefore, &v.ReminderTime, &v.Enabled, &v.Revision, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func getRule(ctx context.Context, q queryer, uid, id int64) (Rule, error) {
	v, e := scanRule(q.QueryRowContext(ctx, "SELECT "+ruleColumns+" FROM reminder_rules r JOIN service_periods p ON p.id=r.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND r.id=?", uid, id))
	return v, dbError(e)
}
func validRule(v Rule, p Period) error {
	if v.DaysBefore < 0 || len(v.ReminderTime) != 5 || v.ReminderTime[2] != ':' {
		return ErrInvalid
	}
	for _, i := range []int{0, 1, 3, 4} {
		if v.ReminderTime[i] < '0' || v.ReminderTime[i] > '9' {
			return ErrInvalid
		}
	}
	if v.ReminderTime[:2] > "23" || v.ReminderTime[3:] > "59" {
		return ErrInvalid
	}
	if v.Enabled {
		if _, e := domain.ReminderAt(expiration(p), int64(v.DaysBefore), v.ReminderTime); e != nil {
			return ErrInvalid
		}
	}
	return nil
}
func uniqueRule(ctx context.Context, tx *sql.Tx, v Rule) error {
	var count int
	if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM reminder_rules WHERE period_id=? AND days_before=? AND reminder_time=? AND id!=?", v.PeriodID, v.DaysBefore, v.ReminderTime, v.ID).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return ErrConflict
	}
	return nil
}
func (s *Service) CreateRule(ctx context.Context, uid, pid int64, in RuleInput) (Rule, error) {
	v := Rule{PeriodID: pid, DaysBefore: in.DaysBefore, ReminderTime: in.ReminderTime, Enabled: true, Revision: 1, CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix()}
	if in.Enabled != nil {
		v.Enabled = *in.Enabled
	}
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		p, e := s.getPeriod(ctx, tx, uid, pid)
		if e != nil {
			return e
		}
		if e = validRule(v, p); e != nil {
			return e
		}
		if e = uniqueRule(ctx, tx, v); e != nil {
			return e
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO reminder_rules(period_id,days_before,reminder_time,enabled,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, pid, v.DaysBefore, v.ReminderTime, v.Enabled, v.Revision, v.CreatedAt, v.UpdatedAt)
		if e == nil {
			v.ID, e = r.LastInsertId()
		}
		return e
	})
	return v, e
}
func (s *Service) GetRule(ctx context.Context, uid, id int64) (Rule, error) {
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return Rule{}, e
	}
	return getRule(ctx, s.store.DB, uid, id)
}
func (s *Service) ListRules(ctx context.Context, uid, pid, after int64, limit int) (Page[Rule], error) {
	out := Page[Rule]{Data: []Rule{}}
	if e := pageArgs(after, limit); e != nil {
		return out, e
	}
	if _, e := s.GetPeriod(ctx, uid, pid); e != nil {
		return out, e
	}
	rows, e := s.store.DB.QueryContext(ctx, "SELECT "+ruleColumns+" FROM reminder_rules r JOIN service_periods p ON p.id=r.period_id JOIN service_items s ON s.id=p.service_id WHERE s.user_id=? AND p.id=? AND r.id>? ORDER BY r.id LIMIT ?", uid, pid, after, limit+1)
	if e != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanRule(rows)
		if e != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Rule) int64 { return v.ID }), dbError(rows.Err())
}
func (s *Service) UpdateRule(ctx context.Context, uid, id int64, in RulePatch) (Rule, error) {
	var v Rule
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var e error
		v, e = getRule(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Revision != in.ExpectedRevision {
			return ErrConflict
		}
		p, e := s.getPeriod(ctx, tx, uid, v.PeriodID)
		if e != nil {
			return e
		}
		old := v
		if in.DaysBefore != nil {
			v.DaysBefore = *in.DaysBefore
		}
		if in.ReminderTime != nil {
			v.ReminderTime = *in.ReminderTime
		}
		if in.Enabled != nil {
			v.Enabled = *in.Enabled
		}
		if e = validRule(v, p); e != nil {
			return e
		}
		if e = uniqueRule(ctx, tx, v); e != nil {
			return e
		}
		if old.DaysBefore == v.DaysBefore && old.ReminderTime == v.ReminderTime && old.Enabled == v.Enabled {
			return nil
		}
		v.Revision++
		v.UpdatedAt = s.now().Unix()
		_, e = tx.ExecContext(ctx, "UPDATE reminder_rules SET days_before=?,reminder_time=?,enabled=?,revision=?,updated_at=? WHERE id=?", v.DaysBefore, v.ReminderTime, v.Enabled, v.Revision, v.UpdatedAt, id)
		if e != nil {
			return e
		}
		return cancelJobs(ctx, tx, p.ID, &id, v.UpdatedAt)
	})
	return v, e
}

// DeleteRule permanently removes a rule and its reminder jobs.
func (s *Service) DeleteRule(ctx context.Context, uid, id, expected int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		v, e := getRule(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		if v.Revision != expected {
			return ErrConflict
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM reminder_rules WHERE id=? AND revision=?", id, expected)
		return e
	})
}
