package service

import (
	"context"
	"database/sql"
	"time"

	"telegram.bot.remind/internal/domain"
)

// DueSoonDays is the inclusive number of local calendar days that counts as
// "due soon" on the dashboard.
const DueSoonDays = 7

// OverviewCounts summarizes the active cards of one user.
type OverviewCounts struct {
	Total    int
	DueSoon  int
	Expired  int
	NoPeriod int
}

// OverviewItem pairs a card with its primary validity period.
type OverviewItem struct {
	SIM           SIM
	PrimaryPeriod *Period
}

// Overview is one page of cards plus counts over all of the user's active
// cards. Counts are independent of pagination.
type Overview struct {
	Counts OverviewCounts
	Items  Page[OverviewItem]
}

// primaryPeriodSQL selects the earliest non-cancelled period of a card.
const primaryPeriodSQL = `(
	SELECT p2.id FROM validity_periods p2
	WHERE p2.sim_id = s.id AND p2.status != 'cancelled'
	ORDER BY p2.expires_at, p2.id DESC LIMIT 1
)`

// Overview returns the dashboard page and counts inside one read transaction so
// the two views cannot disagree.
func (s *Service) Overview(ctx context.Context, uid, after int64, limit int) (Overview, error) {
	out := Overview{Items: Page[OverviewItem]{Data: []OverviewItem{}}}
	if e := pageArgs(after, limit); e != nil {
		return out, e
	}
	tx, e := s.store.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, ErrUnavailable
	}
	defer tx.Rollback()
	if e = s.authorize(ctx, tx, uid); e != nil {
		return out, e
	}
	var zone string
	if e = tx.QueryRowContext(ctx, "SELECT timezone FROM users WHERE id=?", uid).Scan(&zone); e != nil {
		return out, dbError(e)
	}
	loc, e := time.LoadLocation(zone)
	if e != nil || zone == "" || zone == "Local" {
		return out, ErrUnavailable
	}
	now := s.now()
	if out.Counts, e = overviewCounts(ctx, tx, uid, loc, now); e != nil {
		return out, e
	}
	if out.Items, e = overviewItems(ctx, tx, uid, after, limit, now.Unix()); e != nil {
		return out, e
	}
	return out, nil
}

// overviewCounts reads every active card with its primary period. A single
// query keeps the classification consistent and avoids per-card round trips.
func overviewCounts(ctx context.Context, tx *sql.Tx, uid int64, loc *time.Location, now time.Time) (OverviewCounts, error) {
	var out OverviewCounts
	rows, e := tx.QueryContext(ctx, `SELECT p.id,p.expires_at,p.date_precision,p.local_expires_date
		FROM sims s LEFT JOIN validity_periods p ON p.id=`+primaryPeriodSQL+`
		WHERE s.user_id=? AND s.status='active'`, uid)
	if e != nil {
		return out, dbError(e)
	}
	defer rows.Close()
	nowUnix := now.Unix()
	for rows.Next() {
		var periodID, expiresAt sql.NullInt64
		var precision, localDate sql.NullString
		if e = rows.Scan(&periodID, &expiresAt, &precision, &localDate); e != nil {
			return out, dbError(e)
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
		expiration := domain.Expiration{
			ExpiresAt:        time.Unix(expiresAt.Int64, 0).UTC(),
			DatePrecision:    precision.String,
			LocalExpiresDate: date,
		}
		days, err := domain.CalendarDaysUntil(expiration, now, loc)
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
	if e = rows.Err(); e != nil {
		return out, dbError(e)
	}
	return out, nil
}

// overviewItems pages active cards by ID, with the earliest non-cancelled
// period attached when one exists.
func overviewItems(ctx context.Context, tx *sql.Tx, uid, after int64, limit int, now int64) (Page[OverviewItem], error) {
	page := Page[OverviewItem]{Data: []OverviewItem{}}
	rows, e := tx.QueryContext(ctx, `SELECT s.id,s.user_id,s.name,s.card_type,s.operator,s.phone_number,s.country_or_region,s.iccid,s.notes,s.status,s.created_at,s.updated_at,`+
		periodColumns+` FROM sims s LEFT JOIN validity_periods p ON p.id=`+primaryPeriodSQL+`
		WHERE s.user_id=? AND s.status='active' AND s.id>? ORDER BY s.id LIMIT ?`, uid, after, limit+1)
	if e != nil {
		return page, dbError(e)
	}
	defer rows.Close()
	for rows.Next() {
		var item OverviewItem
		var pid, psim, pstarts, pexpires, pcycle, pcreated, pupdated sql.NullInt64
		var ptype, pname, pprecision, pzone, pstatus, plocal sql.NullString
		if e = rows.Scan(
			&item.SIM.ID, &item.SIM.UserID, &item.SIM.Name, &item.SIM.CardType,
			&item.SIM.Operator, &item.SIM.PhoneNumber, &item.SIM.CountryOrRegion,
			&item.SIM.ICCID, &item.SIM.Notes, &item.SIM.Status,
			&item.SIM.CreatedAt, &item.SIM.UpdatedAt,
			&pid, &psim, &ptype, &pname, &pstarts, &pexpires, &pprecision,
			&plocal, &pzone, &pstatus, &pcycle, &pcreated, &pupdated,
		); e != nil {
			return page, dbError(e)
		}
		if pid.Valid {
			if !psim.Valid || !pexpires.Valid || !pcycle.Valid || !pcreated.Valid || !pupdated.Valid ||
				!ptype.Valid || !pname.Valid || !pprecision.Valid || !pzone.Valid || !pstatus.Valid {
				return page, ErrUnavailable
			}
			period := Period{
				ID: pid.Int64, SIMID: psim.Int64, PeriodType: ptype.String, Name: pname.String,
				ExpiresAt: pexpires.Int64, DatePrecision: pprecision.String, Timezone: pzone.String,
				Status: status(pexpires.Int64, now), Cycle: pcycle.Int64,
				CreatedAt: pcreated.Int64, UpdatedAt: pupdated.Int64,
			}
			if pstarts.Valid {
				period.StartsAt = &pstarts.Int64
			}
			if plocal.Valid {
				period.LocalExpiresDate = &plocal.String
			}
			item.PrimaryPeriod = &period
		}
		page.Data = append(page.Data, item)
	}
	if e = rows.Err(); e != nil {
		return page, dbError(e)
	}
	return finishPage(page.Data, limit, func(v OverviewItem) int64 { return v.SIM.ID }), nil
}
