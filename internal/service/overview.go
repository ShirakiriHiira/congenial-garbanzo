package service

import (
	"context"
	"database/sql"
	"time"

	"telegram.bot.remind/internal/domain"
)

const DueSoonDays = 7

type OverviewCounts struct {
	Total    int
	DueSoon  int
	Expired  int
	NoPeriod int
}
type OverviewItem struct {
	SIM           SIM
	PrimaryPeriod *Period
}
type Overview struct {
	Counts OverviewCounts
	Items  Page[OverviewItem]
}

const primaryPeriodSQL = `(
    SELECT p2.id FROM validity_periods p2
    WHERE p2.service_id = s.id AND p2.status != 'cancelled'
    ORDER BY p2.expires_at, p2.id DESC LIMIT 1
)`

func (s *Service) Overview(ctx context.Context, uid, after int64, limit int) (Overview, error) {
	out := Overview{Items: Page[OverviewItem]{Data: []OverviewItem{}}}
	if err := pageArgs(after, limit); err != nil {
		return out, err
	}
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, ErrUnavailable
	}
	defer tx.Rollback()
	if err = s.authorize(ctx, tx, uid); err != nil {
		return out, err
	}
	var zone string
	if err = tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&zone); err != nil {
		return out, dbError(err)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" {
		return out, ErrUnavailable
	}
	now := s.now()
	if out.Counts, err = overviewCounts(ctx, tx, uid, loc, now); err != nil {
		return out, err
	}
	if out.Items, err = overviewItems(ctx, tx, uid, after, limit, now.Unix()); err != nil {
		return out, err
	}
	return out, nil
}

func overviewCounts(ctx context.Context, tx *sql.Tx, uid int64, loc *time.Location, now time.Time) (OverviewCounts, error) {
	var out OverviewCounts
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.expires_at,p.date_precision,p.local_expires_date FROM service_items s JOIN sim_details sd ON sd.service_id=s.id LEFT JOIN validity_periods p ON p.id=`+primaryPeriodSQL+` WHERE s.user_id=? AND s.status='active'`, uid)
	if err != nil {
		return out, dbError(err)
	}
	defer rows.Close()
	nowUnix := now.Unix()
	for rows.Next() {
		var periodID, expiresAt sql.NullInt64
		var precision, localDate sql.NullString
		if err = rows.Scan(&periodID, &expiresAt, &precision, &localDate); err != nil {
			return out, dbError(err)
		}
		out.Total++
		if !periodID.Valid {
			out.NoPeriod++
			continue
		}
		if !expiresAt.Valid || !precision.Valid {
			return out, ErrUnavailable
		}
		var date *string
		if localDate.Valid {
			date = &localDate.String
		}
		exp := domain.Expiration{ExpiresAt: time.Unix(expiresAt.Int64, 0).UTC(), DatePrecision: precision.String, LocalExpiresDate: date}
		days, err := domain.CalendarDaysUntil(exp, now, loc)
		if err != nil {
			return out, ErrUnavailable
		}
		switch {
		case expiresAt.Int64 <= nowUnix:
			out.Expired++
		case days >= 0 && days <= DueSoonDays:
			out.DueSoon++
		}
	}
	if err = rows.Err(); err != nil {
		return out, dbError(err)
	}
	return out, nil
}

func overviewItems(ctx context.Context, tx *sql.Tx, uid, after int64, limit int, now int64) (Page[OverviewItem], error) {
	page := Page[OverviewItem]{Data: []OverviewItem{}}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.user_id,s.name,s.category_id,s.provider,s.identifier,s.management_url,s.notes,s.status,s.created_at,s.updated_at,sd.card_type,sd.phone_number,sd.country_or_region,sd.iccid,`+periodColumns+` FROM service_items s JOIN sim_details sd ON sd.service_id=s.id LEFT JOIN validity_periods p ON p.id=`+primaryPeriodSQL+` LEFT JOIN period_terms t ON t.validity_period_id=p.id LEFT JOIN sim_details d ON d.service_id=p.service_id WHERE s.user_id=? AND s.status='active' AND s.id>? ORDER BY s.id LIMIT ?`, uid, after, limit+1)
	if err != nil {
		return page, dbError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sr serviceRow
		var pid, pservice, pstarts, pexpires, pcycle, pcreated, pupdated, psim sql.NullInt64
		var ptype, pname, pprecision, plocal, pzone, pstatus, pmode, punit, pcurrency sql.NullString
		var pcount, pamount, prev sql.NullInt64
		args := append(sr.args(), &pid, &pservice, &ptype, &pname, &pstarts, &pexpires, &pprecision, &plocal, &pzone, &pstatus, &pcycle, &pcreated, &pupdated, &psim, &pmode, &pcount, &punit, &pamount, &pcurrency, &prev)
		if err = rows.Scan(args...); err != nil {
			return page, dbError(err)
		}
		sim, err := simFromService(sr.value())
		if err != nil {
			return page, ErrUnavailable
		}
		item := OverviewItem{SIM: sim}
		if pid.Valid {
			if !pservice.Valid || !ptype.Valid || !pname.Valid || !pexpires.Valid || !pprecision.Valid || !pzone.Valid || !pstatus.Valid || !pcycle.Valid || !pcreated.Valid || !pupdated.Valid {
				return page, ErrUnavailable
			}
			period := Period{ID: pid.Int64, ServiceID: pservice.Int64, PeriodType: ptype.String, Name: pname.String, ExpiresAt: pexpires.Int64, DatePrecision: pprecision.String, Timezone: pzone.String, Status: status(pexpires.Int64, now), Cycle: pcycle.Int64, CreatedAt: pcreated.Int64, UpdatedAt: pupdated.Int64, Terms: PeriodTerms{Mode: "custom", Revision: 1}}
			if psim.Valid {
				x := psim.Int64
				period.SIMID = &x
			}
			if pstarts.Valid {
				period.StartsAt = &pstarts.Int64
			}
			if plocal.Valid {
				period.LocalExpiresDate = &plocal.String
			}
			if pmode.Valid {
				period.Terms.Mode = pmode.String
				period.Terms.Revision = prev.Int64
				if pcount.Valid {
					period.Terms.IntervalCount = &pcount.Int64
				}
				if punit.Valid {
					period.Terms.IntervalUnit = &punit.String
				}
				if pamount.Valid {
					period.Terms.Amount = &pamount.Int64
				}
				if pcurrency.Valid {
					period.Terms.Currency = &pcurrency.String
				}
			}
			item.PrimaryPeriod = &period
		}
		page.Data = append(page.Data, item)
	}
	if err = rows.Err(); err != nil {
		return page, dbError(err)
	}
	return finishPage(page.Data, limit, func(v OverviewItem) int64 { return v.SIM.ID }), nil
}
