-- Rename the existing graph in place so IDs, leases and retired IDs survive.
CREATE TABLE service_categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE RESTRICT,
    code TEXT UNIQUE,
    name TEXT NOT NULL CHECK (name = trim(name) AND length(name) BETWEEN 1 AND 200),
    kind TEXT NOT NULL CHECK (kind IN ('sim','generic')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (
        (code IS NULL AND user_id IS NOT NULL AND kind = 'generic') OR
        (code IS NOT NULL AND user_id IS NULL AND (
            (code = 'sim' AND name = 'SIM 与 eSIM' AND kind = 'sim') OR
            (code = 'vps' AND name = 'VPS 与云服务器' AND kind = 'generic') OR
            (code = 'subscription' AND name = '软件与 AI 订阅' AND kind = 'generic') OR
            (code = 'domain' AND name = '域名' AND kind = 'generic') OR
            (code = 'other' AND name = '其他' AND kind = 'generic')
        ))
    )
);
CREATE UNIQUE INDEX service_categories_user_name ON service_categories(user_id,name) WHERE user_id IS NOT NULL;
INSERT INTO service_categories(code,name,kind,created_at,updated_at) VALUES
    ('sim','SIM 与 eSIM','sim',unixepoch(),unixepoch()),
    ('vps','VPS 与云服务器','generic',unixepoch(),unixepoch()),
    ('subscription','软件与 AI 订阅','generic',unixepoch(),unixepoch()),
    ('domain','域名','generic',unixepoch(),unixepoch()),
    ('other','其他','generic',unixepoch(),unixepoch());

CREATE TRIGGER service_categories_protect_update BEFORE UPDATE ON service_categories
WHEN OLD.user_id IS NULL OR NEW.id IS NOT OLD.id OR NEW.user_id IS NOT OLD.user_id
    OR NEW.code IS NOT OLD.code OR NEW.kind IS NOT OLD.kind OR NEW.created_at IS NOT OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'category identity is immutable');
END;
CREATE TRIGGER service_categories_protect_delete BEFORE DELETE ON service_categories
WHEN OLD.user_id IS NULL
BEGIN
    SELECT RAISE(ABORT, 'built-in category is read-only');
END;

ALTER TABLE sims RENAME TO service_items;
ALTER TABLE service_items RENAME COLUMN operator TO provider;
-- WITHOUT ROWID keeps the parent ID as the real primary key: an INTEGER
-- PRIMARY KEY in a rowid table is a rowid alias that silently accepts NULL and
-- could attach details to an unrelated parent.
CREATE TABLE sim_details (
    service_id INTEGER PRIMARY KEY REFERENCES service_items(id) ON DELETE CASCADE,
    card_type TEXT NOT NULL CHECK (card_type IN ('physical_sim','esim')),
    phone_number TEXT,
    country_or_region TEXT,
    iccid TEXT
) WITHOUT ROWID;
INSERT INTO sim_details(service_id,card_type,phone_number,country_or_region,iccid)
    SELECT id,card_type,phone_number,country_or_region,iccid FROM service_items;
ALTER TABLE service_items DROP COLUMN card_type;
ALTER TABLE service_items DROP COLUMN phone_number;
ALTER TABLE service_items DROP COLUMN country_or_region;
ALTER TABLE service_items DROP COLUMN iccid;
ALTER TABLE service_items ADD COLUMN identifier TEXT;
ALTER TABLE service_items ADD COLUMN management_url TEXT;
ALTER TABLE service_items ADD COLUMN category_id INTEGER REFERENCES service_categories(id) ON DELETE RESTRICT;
UPDATE service_items SET category_id = (SELECT id FROM service_categories WHERE code = 'sim');
DROP INDEX sims_user_id_id;
CREATE INDEX service_items_user_id_id ON service_items(user_id,id);

-- SQLite cannot ADD a NOT NULL REFERENCES column with a non-NULL default.
CREATE TRIGGER service_items_category_insert BEFORE INSERT ON service_items
WHEN NEW.category_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM service_categories c WHERE c.id = NEW.category_id
        AND (c.user_id IS NULL OR c.user_id = NEW.user_id)
)
BEGIN
    SELECT RAISE(ABORT, 'category is required and must be visible');
END;
CREATE TRIGGER service_items_category_update BEFORE UPDATE OF category_id,user_id ON service_items
WHEN NEW.category_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM service_categories c WHERE c.id = NEW.category_id
        AND (c.user_id IS NULL OR c.user_id = NEW.user_id)
) OR (SELECT kind FROM service_categories WHERE id = OLD.category_id)
    IS NOT (SELECT kind FROM service_categories WHERE id = NEW.category_id)
BEGIN
    SELECT RAISE(ABORT, 'category must be visible and retain its kind');
END;

ALTER TABLE validity_periods RENAME COLUMN sim_id TO service_id;
DROP INDEX validity_periods_sim_id_id;
CREATE INDEX validity_periods_service_id_id ON validity_periods(service_id,id);
CREATE TABLE period_terms (
    validity_period_id INTEGER PRIMARY KEY REFERENCES validity_periods(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('once','recurring','custom')),
    interval_count INTEGER,
    interval_unit TEXT,
    amount INTEGER,
    currency TEXT,
    revision INTEGER NOT NULL CHECK (typeof(revision) = 'integer' AND revision >= 1),
    CHECK (
        (mode = 'recurring' AND interval_count IS NOT NULL
            AND typeof(interval_count) = 'integer' AND interval_count > 0
            AND interval_unit IS NOT NULL AND interval_unit IN ('day','week','month','year')) OR
        (mode IN ('once','custom') AND interval_count IS NULL AND interval_unit IS NULL)
    ),
    CHECK (
        (amount IS NULL AND currency IS NULL) OR
        (amount IS NOT NULL AND typeof(amount) = 'integer' AND amount >= 0
            AND currency IS NOT NULL AND currency GLOB '[A-Z][A-Z][A-Z]')
    )
) WITHOUT ROWID;
