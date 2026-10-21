package service

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
)

const serviceColumns = `s.id,s.user_id,s.name,s.category_id,s.provider,s.identifier,s.management_url,s.notes,s.status,s.created_at,s.updated_at,d.card_type,d.phone_number,d.country_or_region,d.iccid`

// serviceRow keeps nullable SIM detail columns separate until the complete row
// has been scanned. A missing card_type is the database identity of a generic
// service, not an empty SIM detail object.
type serviceRow struct {
	item     ServiceItem
	cardType sql.NullString
	phone    sql.NullString
	country  sql.NullString
	iccid    sql.NullString
}

func (r *serviceRow) args() []any {
	return []any{
		&r.item.ID, &r.item.UserID, &r.item.Name, &r.item.CategoryID,
		&r.item.Provider, &r.item.Identifier, &r.item.ManagementURL,
		&r.item.Notes, &r.item.Status, &r.item.CreatedAt, &r.item.UpdatedAt,
		&r.cardType, &r.phone, &r.country, &r.iccid,
	}
}

func (r *serviceRow) value() ServiceItem {
	if r.cardType.Valid {
		r.item.SIMDetails = &SIMDetails{CardType: r.cardType.String}
		if r.phone.Valid {
			r.item.SIMDetails.PhoneNumber = &r.phone.String
		}
		if r.country.Valid {
			r.item.SIMDetails.CountryOrRegion = &r.country.String
		}
		if r.iccid.Valid {
			r.item.SIMDetails.ICCID = &r.iccid.String
		}
	}
	return r.item
}

func getServiceItem(ctx context.Context, q rowQuerier, uid, id int64) (ServiceItem, error) {
	var r serviceRow
	err := q.QueryRowContext(ctx, "SELECT "+serviceColumns+" FROM service_items s LEFT JOIN sim_details d ON d.service_id=s.id WHERE s.user_id=? AND s.id=?", uid, id).Scan(r.args()...)
	if err != nil {
		return ServiceItem{}, dbError(err)
	}
	return r.value(), nil
}

func validSIMDetails(v *SIMDetails) bool {
	return v != nil && (v.CardType == "physical_sim" || v.CardType == "esim") &&
		optionalText(v.PhoneNumber, 200) && optionalText(v.CountryOrRegion, 200) && optionalText(v.ICCID, 200)
}

func validManagementURL(v *string) bool {
	if v == nil {
		return true
	}
	if !text(*v, 2048, false) {
		return false
	}
	u, err := url.Parse(*v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

func validService(v ServiceItem) bool {
	return text(v.Name, 200, true) && optionalText(v.Provider, 200) && optionalText(v.Identifier, 200) &&
		validManagementURL(v.ManagementURL) && optionalText(v.Notes, 4000) &&
		(v.Status == "active" || v.Status == "archived") &&
		(v.SIMDetails == nil || validSIMDetails(v.SIMDetails))
}

func categoryKind(ctx context.Context, q rowQuerier, uid, id int64) (string, error) {
	var owner sql.NullInt64
	var kind string
	err := q.QueryRowContext(ctx, "SELECT user_id,kind FROM service_categories WHERE id=?", id).Scan(&owner, &kind)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", dbError(err)
	}
	if owner.Valid && owner.Int64 != uid {
		return "", ErrNotFound
	}
	return kind, nil
}

func (s *Service) CreateService(ctx context.Context, uid int64, in ServiceInput) (ServiceItem, error) {
	v := ServiceItem{
		UserID: uid, Name: strings.TrimSpace(in.Name), CategoryID: in.CategoryID,
		Provider: in.Provider, Identifier: in.Identifier, ManagementURL: in.ManagementURL,
		Notes: in.Notes, Status: "active", CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix(),
		SIMDetails: in.SIMDetails,
	}
	if !validService(v) || v.CategoryID < 1 {
		return v, ErrInvalid
	}
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		kind, err := categoryKind(ctx, tx, uid, v.CategoryID)
		if err != nil {
			return err
		}
		if (kind == "sim") != (v.SIMDetails != nil) {
			return ErrInvalid
		}
		// A SIM service identifies itself through sim_details; the generic
		// identifier belongs to every other category only.
		if kind == "sim" && v.Identifier != nil {
			return ErrInvalid
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO service_items(user_id,name,category_id,provider,identifier,management_url,notes,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, uid, v.Name, v.CategoryID, v.Provider, v.Identifier, v.ManagementURL, v.Notes, v.Status, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			return err
		}
		v.ID, err = r.LastInsertId()
		if err != nil {
			return err
		}
		if v.SIMDetails != nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO sim_details(service_id,card_type,phone_number,country_or_region,iccid) VALUES(?,?,?,?,?)`, v.ID, v.SIMDetails.CardType, v.SIMDetails.PhoneNumber, v.SIMDetails.CountryOrRegion, v.SIMDetails.ICCID)
		}
		return err
	})
	return v, err
}

func (s *Service) GetService(ctx context.Context, uid, id int64) (ServiceItem, error) {
	if err := s.authorize(ctx, s.store.DB, uid); err != nil {
		return ServiceItem{}, err
	}
	return getServiceItem(ctx, s.store.DB, uid, id)
}

func applySIMDetailsPatch(v *SIMDetails, in SIMDetailsPatch) error {
	if v == nil {
		return ErrInvalid
	}
	if in.CardType.Set {
		if in.CardType.Value == nil {
			return ErrInvalid
		}
		v.CardType = *in.CardType.Value
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
	return nil
}

func (s *Service) UpdateService(ctx context.Context, uid, id int64, in ServicePatch) (ServiceItem, error) {
	var v ServiceItem
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var err error
		v, err = getServiceItem(ctx, tx, uid, id)
		if err != nil {
			return err
		}
		oldStatus := v.Status
		oldKind, err := categoryKind(ctx, tx, uid, v.CategoryID)
		if err != nil {
			return err
		}
		if in.Name != nil {
			v.Name = strings.TrimSpace(*in.Name)
		}
		if in.CategoryID != nil {
			kind, e := categoryKind(ctx, tx, uid, *in.CategoryID)
			if e != nil {
				return e
			}
			if kind != oldKind {
				return ErrConflict
			}
			v.CategoryID = *in.CategoryID
		}
		if in.Provider.Set {
			v.Provider = in.Provider.Value
		}
		if in.Identifier.Set {
			if oldKind == "sim" && in.Identifier.Value != nil {
				return ErrInvalid
			}
			v.Identifier = in.Identifier.Value
		}
		if in.ManagementURL.Set {
			v.ManagementURL = in.ManagementURL.Value
		}
		if in.Notes.Set {
			v.Notes = in.Notes.Value
		}
		if in.Status != nil {
			v.Status = *in.Status
		}
		if in.SIMDetailsSet {
			if oldKind != "sim" {
				if in.SIMDetails != nil {
					return ErrInvalid
				}
			} else {
				if in.SIMDetails == nil {
					return ErrInvalid
				}
				if err = applySIMDetailsPatch(v.SIMDetails, *in.SIMDetails); err != nil {
					return err
				}
			}
		}
		if (oldKind == "sim") != (v.SIMDetails != nil) || !validService(v) {
			return ErrInvalid
		}
		v.UpdatedAt = s.now().Unix()
		if _, err = tx.ExecContext(ctx, `UPDATE service_items SET name=?,category_id=?,provider=?,identifier=?,management_url=?,notes=?,status=?,updated_at=? WHERE id=? AND user_id=?`, v.Name, v.CategoryID, v.Provider, v.Identifier, v.ManagementURL, v.Notes, v.Status, v.UpdatedAt, id, uid); err != nil {
			return err
		}
		if v.SIMDetails != nil {
			if _, err = tx.ExecContext(ctx, `UPDATE sim_details SET card_type=?,phone_number=?,country_or_region=?,iccid=? WHERE service_id=?`, v.SIMDetails.CardType, v.SIMDetails.PhoneNumber, v.SIMDetails.CountryOrRegion, v.SIMDetails.ICCID, id); err != nil {
				return err
			}
		}
		if oldStatus != v.Status {
			if _, err = tx.ExecContext(ctx, `UPDATE reminder_jobs SET status='cancelled',next_attempt_at=NULL,updated_at=? WHERE period_id IN (SELECT id FROM service_periods WHERE service_id=?) AND status IN('pending','failed','sending')`, v.UpdatedAt, id); err != nil {
				return err
			}
			if v.Status == "active" {
				_, err = tx.ExecContext(ctx, `UPDATE service_periods SET cycle=cycle+1,updated_at=? WHERE service_id=?`, v.UpdatedAt, id)
			}
			// An archived service cannot continue a wizard against it; drop
			// drafts atomically with the state change.
			if err = s.CancelDialogsForTarget(ctx, tx, uid, id, 0, 0); err != nil {
				return err
			}
		}
		return err
	})
	return v, err
}

func (s *Service) DeleteService(ctx context.Context, uid, id int64) error {
	return s.transaction(ctx, uid, func(tx *sql.Tx) error {
		if _, err := getServiceItem(ctx, tx, uid, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM service_items WHERE id=? AND user_id=?", id, uid)
		return err
	})
}

func scanCategory(row scanner) (Category, error) {
	var v Category
	err := row.Scan(&v.ID, &v.UserID, &v.Code, &v.Name, &v.Kind, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Service) CreateCategory(ctx context.Context, uid int64, name string) (Category, error) {
	name = strings.TrimSpace(name)
	v := Category{UserID: &uid, Name: name, Kind: "generic", CreatedAt: s.now().Unix(), UpdatedAt: s.now().Unix()}
	if !text(name, 200, true) {
		return v, ErrInvalid
	}
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `INSERT INTO service_categories(user_id,code,name,kind,created_at,updated_at) VALUES(?,NULL,?,?,?,?)`, uid, v.Name, v.Kind, v.CreatedAt, v.UpdatedAt)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return ErrConflict
			}
			return err
		}
		v.ID, err = r.LastInsertId()
		return err
	})
	return v, err
}

func (s *Service) UpdateCategory(ctx context.Context, uid, id int64, name string) (Category, error) {
	name = strings.TrimSpace(name)
	if !text(name, 200, true) {
		return Category{}, ErrInvalid
	}
	var v Category
	err := s.transaction(ctx, uid, func(tx *sql.Tx) error {
		var owner sql.NullInt64
		var code sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT id,user_id,code,name,kind,created_at,updated_at FROM service_categories WHERE id=?`, id).Scan(&v.ID, &owner, &code, &v.Name, &v.Kind, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return dbError(err)
		}
		if !owner.Valid {
			return ErrConflict
		}
		if owner.Int64 != uid {
			return ErrNotFound
		}
		v.UserID = &owner.Int64
		if code.Valid {
			v.Code = &code.String
		}
		v.Name = name
		v.UpdatedAt = s.now().Unix()
		_, err := tx.ExecContext(ctx, "UPDATE service_categories SET name=?,updated_at=? WHERE id=? AND user_id=?", v.Name, v.UpdatedAt, id, uid)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrConflict
		}
		return err
	})
	return v, err
}

func (s *Service) ListCategories(ctx context.Context, uid, after int64, limit int) (Page[Category], error) {
	out := Page[Category]{Data: []Category{}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	if err := s.authorize(ctx, s.store.DB, uid); err != nil {
		return out, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,user_id,code,name,kind,created_at,updated_at FROM service_categories WHERE id>? AND (user_id IS NULL OR user_id=?) ORDER BY id LIMIT ?`, after, uid, limit+1)
	if err != nil {
		return out, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanCategory(rows)
		if err != nil {
			return out, ErrUnavailable
		}
		out.Data = append(out.Data, v)
	}
	return finishPage(out.Data, limit, func(v Category) int64 { return v.ID }), dbError(rows.Err())
}
