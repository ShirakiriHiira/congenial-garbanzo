package service

import (
	"context"
	"database/sql"
)

func simFromService(v ServiceItem) (SIM, error) {
	if v.SIMDetails == nil {
		return SIM{}, ErrNotFound
	}
	return SIM{ID: v.ID, UserID: v.UserID, Name: v.Name, CardType: v.SIMDetails.CardType,
		Operator: v.Provider, PhoneNumber: v.SIMDetails.PhoneNumber,
		CountryOrRegion: v.SIMDetails.CountryOrRegion, ICCID: v.SIMDetails.ICCID,
		Notes: v.Notes, Status: v.Status, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, nil
}

func getSIM(ctx context.Context, q queryer, uid, id int64) (SIM, error) {
	var r serviceRow
	if err := q.QueryRowContext(ctx, "SELECT "+serviceColumns+" FROM service_items s JOIN sim_details d ON d.service_id=s.id WHERE s.user_id=? AND s.id=?", uid, id).Scan(r.args()...); err != nil {
		return SIM{}, dbError(err)
	}
	return simFromService(r.value())
}

func simCategoryID(ctx context.Context, q queryer) (int64, error) {
	var id int64
	if err := q.QueryRowContext(ctx, "SELECT id FROM service_categories WHERE code='sim'").Scan(&id); err != nil {
		return 0, dbError(err)
	}
	return id, nil
}

func (s *Service) CreateSIM(ctx context.Context, uid int64, in SIMInput) (SIM, error) {
	categoryID, err := simCategoryID(ctx, s.store.DB)
	if err != nil {
		return SIM{}, err
	}
	item, err := s.CreateService(ctx, uid, ServiceInput{CategoryID: categoryID, Name: in.Name, Provider: in.Operator, Notes: in.Notes,
		SIMDetails: &SIMDetails{CardType: in.CardType, PhoneNumber: in.PhoneNumber, CountryOrRegion: in.CountryOrRegion, ICCID: in.ICCID}})
	if err != nil {
		return SIM{}, err
	}
	return simFromService(item)
}

func (s *Service) GetSIM(ctx context.Context, uid, id int64) (SIM, error) {
	if err := s.authorize(ctx, s.store.DB, uid); err != nil {
		return SIM{}, err
	}
	return getSIM(ctx, s.store.DB, uid, id)
}

func (s *Service) ListSIMs(ctx context.Context, uid, after int64, limit int) (Page[SIM], error) {
	out := Page[SIM]{Data: []SIM{}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	if err := s.authorize(ctx, s.store.DB, uid); err != nil {
		return out, err
	}
	rows, err := s.store.DB.QueryContext(ctx, "SELECT "+serviceColumns+" FROM service_items s JOIN sim_details d ON d.service_id=s.id WHERE s.user_id=? AND s.id>? ORDER BY s.id LIMIT ?", uid, after, limit+1)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return out, ErrUnavailable
		}
		sim, err := simFromService(item)
		if err != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, sim)
	}
	return finishPage(out.Data, limit, func(v SIM) int64 { return v.ID }), dbError(rows.Err())
}

func (s *Service) UpdateSIM(ctx context.Context, uid, id int64, in SIMPatch) (SIM, error) {
	if _, err := s.GetSIM(ctx, uid, id); err != nil {
		return SIM{}, err
	}
	details := SIMDetailsPatch{CardType: Optional[string]{Set: in.CardType != nil, Value: in.CardType}, PhoneNumber: in.PhoneNumber, CountryOrRegion: in.CountryOrRegion, ICCID: in.ICCID}
	item, err := s.UpdateService(ctx, uid, id, ServicePatch{Name: in.Name, Provider: in.Operator, Notes: in.Notes, Status: in.Status,
		SIMDetails: Optional[SIMDetailsPatch]{Set: in.CardType != nil || in.PhoneNumber.Set || in.CountryOrRegion.Set || in.ICCID.Set, Value: &details}})
	if err != nil {
		return SIM{}, err
	}
	return simFromService(item)
}

func (s *Service) ArchiveSIM(ctx context.Context, uid, id int64) error {
	status := "archived"
	_, err := s.UpdateSIM(ctx, uid, id, SIMPatch{Status: &status})
	return err
}

// DeleteSIM permanently removes a card and its cascading period data.
func (s *Service) DeleteSIM(ctx context.Context, uid, id int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		if _, err := getSIM(ctx, tx, uid, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM service_items WHERE id=? AND user_id=?", id, uid)
		return err
	})
}
