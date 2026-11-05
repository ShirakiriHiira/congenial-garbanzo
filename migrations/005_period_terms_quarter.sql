-- Renewal intervals may be recorded in quarters. SQLite cannot change a CHECK
-- constraint in place, so rebuild the table and copy its rows.
CREATE TABLE period_terms_new (
    period_id INTEGER PRIMARY KEY REFERENCES service_periods(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('once','recurring','custom')),
    interval_count INTEGER,
    interval_unit TEXT,
    amount INTEGER,
    currency TEXT,
    revision INTEGER NOT NULL CHECK (typeof(revision) = 'integer' AND revision >= 1),
    CHECK (
        (mode = 'recurring' AND interval_count IS NOT NULL
            AND typeof(interval_count) = 'integer' AND interval_count > 0
            AND interval_unit IS NOT NULL AND interval_unit IN ('day','week','month','quarter','year')) OR
        (mode IN ('once','custom') AND interval_count IS NULL AND interval_unit IS NULL)
    ),
    CHECK (
        (amount IS NULL AND currency IS NULL) OR
        (amount IS NOT NULL AND typeof(amount) = 'integer' AND amount >= 0
            AND currency IS NOT NULL AND currency GLOB '[A-Z][A-Z][A-Z]')
    )
) WITHOUT ROWID;

INSERT INTO period_terms_new(period_id,mode,interval_count,interval_unit,amount,currency,revision)
SELECT period_id,mode,interval_count,interval_unit,amount,currency,revision FROM period_terms;

DROP TABLE period_terms;
ALTER TABLE period_terms_new RENAME TO period_terms;
