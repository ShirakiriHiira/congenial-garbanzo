package service

import (
	"context"
	"database/sql"
	"strings"
)

const simColumns = `id,user_id,name,card_type,operator,phone_number,country_or_region,iccid,notes,status,created_at,updated_at`

type scanner interface{ Scan(...any) error }

func scanSIM(row scanner) (SIM, error) {
	var v SIM
	e := row.Scan(&v.ID, &v.UserID, &v.Name, &v.CardType, &v.Operator, &v.PhoneNumber, &v.CountryOrRegion, &v.ICCID, &v.Notes, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func getSIM(ctx context.Context, q queryer, uid, id int64) (SIM, error) {
	v, e := scanSIM(q.QueryRowContext(ctx, "SELECT "+simColumns+" FROM sims WHERE user_id=? AND id=?", uid, id))
	return v, dbError(e)
}
func validSIM(v SIM) bool {
	return text(v.Name, 200, true) && (v.CardType == "esim" || v.CardType == "physical_sim") && optionalText(v.Operator, 200) && optionalText(v.PhoneNumber, 200) && optionalText(v.CountryOrRegion, 200) && optionalText(v.ICCID, 200) && optionalText(v.Notes, 4000) && (v.Status == "active" || v.Status == "archived")
}
func (s *Service) CreateSIM(ctx context.Context, uid int64, in SIMInput) (SIM, error) {
	v := SIM{UserID: uid, Name: strings.TrimSpace(in.Name), CardType: in.CardType, Operator: in.Operator, PhoneNumber: in.PhoneNumber, CountryOrRegion: in.CountryOrRegion, ICCID: in.ICCID, Notes: in.Notes, Status: "active", CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix()}
	if !validSIM(v) {
		return v, ErrInvalid
	}
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `INSERT INTO sims(user_id,name,card_type,operator,phone_number,country_or_region,iccid,notes,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, uid, v.Name, v.CardType, v.Operator, v.PhoneNumber, v.CountryOrRegion, v.ICCID, v.Notes, v.Status, v.CreatedAt, v.UpdatedAt)
		if e == nil {
			v.ID, e = r.LastInsertId()
		}
		return e
	})
	return v, e
}
func (s *Service) GetSIM(ctx context.Context, uid, id int64) (SIM, error) {
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return SIM{}, e
	}
	return getSIM(ctx, s.store.DB, uid, id)
}
func (s *Service) ListSIMs(ctx context.Context, uid, after int64, limit int) (Page[SIM], error) {
	empty := Page[SIM]{Data: []SIM{}}
	if e := pageArgs(after, limit); e != nil {
		return empty, e
	}
	if e := s.authorize(ctx, s.store.DB, uid); e != nil {
		return empty, e
	}
	rows, e := s.store.DB.QueryContext(ctx, "SELECT "+simColumns+" FROM sims WHERE user_id=? AND id>? ORDER BY id LIMIT ?", uid, after, limit+1)
	if e != nil {
		return empty, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanSIM(rows)
		if e != nil {
			return empty, ErrUnavailable
		}
		empty.Data = append(empty.Data, v)
	}
	return finishPage(empty.Data, limit, func(v SIM) int64 { return v.ID }), dbError(rows.Err())
}
func (s *Service) UpdateSIM(ctx context.Context, uid, id int64, in SIMPatch) (SIM, error) {
	var v SIM
	e := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var e error
		v, e = getSIM(ctx, tx, uid, id)
		if e != nil {
			return e
		}
		old := v.Status
		if in.Name != nil {
			v.Name = strings.TrimSpace(*in.Name)
		}
		if in.CardType != nil {
			v.CardType = *in.CardType
		}
		if in.Status != nil {
			v.Status = *in.Status
		}
		if in.Operator.Set {
			v.Operator = in.Operator.Value
		}
		if in.PhoneNumber.Set {
			v.PhoneNumber = in.PhoneNumber.Value
		}
		if in.CountryOrRegion.Set {
			v.CountryOrRegion = in.CountryOrRegion.Value
		}
		if in.ICCID.Set {
			v.ICCID = in.ICCID.Value
		}
		if in.Notes.Set {
			v.Notes = in.Notes.Value
		}
		if !validSIM(v) {
			return ErrInvalid
		}
		v.UpdatedAt = s.now().Unix()
		_, e = tx.ExecContext(ctx, `UPDATE sims SET name=?,card_type=?,operator=?,phone_number=?,country_or_region=?,iccid=?,notes=?,status=?,updated_at=? WHERE id=? AND user_id=?`, v.Name, v.CardType, v.Operator, v.PhoneNumber, v.CountryOrRegion, v.ICCID, v.Notes, v.Status, v.UpdatedAt, id, uid)
		if e != nil {
			return e
		}
		if old != v.Status {
			_, e = tx.ExecContext(ctx, `UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,updated_at=? WHERE validity_period_id IN(SELECT id FROM validity_periods WHERE sim_id=?) AND status IN('pending','failed','sending')`, v.UpdatedAt, id)
			if e != nil {
				return e
			}
			if v.Status == "active" {
				_, e = tx.ExecContext(ctx, `UPDATE validity_periods SET cycle=cycle+1,updated_at=? WHERE sim_id=? AND status!='cancelled'`, v.UpdatedAt, id)
			}
		}
		return e
	})
	return v, e
}
func (s *Service) ArchiveSIM(ctx context.Context, uid, id int64) error {
	v := "archived"
	_, e := s.UpdateSIM(ctx, uid, id, SIMPatch{Status: &v})
	return e
}

// DeleteSIM permanently removes a card and its cascading period data.
func (s *Service) DeleteSIM(ctx context.Context, uid, id int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		if _, e := getSIM(ctx, tx, uid, id); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "DELETE FROM sims WHERE id=? AND user_id=?", id, uid)
		return e
	})
}
