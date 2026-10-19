-- Clean baseline: generic services and subscriptions with multiple periods.
-- Older databases are intentionally rejected by the migration checksum
-- ledger; this file is never applied on top of the legacy schema.

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_user_id INTEGER NOT NULL UNIQUE CHECK (telegram_user_id > 0),
    timezone TEXT NOT NULL,
    private_chat_id INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

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

-- Built-in categories are read-only; custom identities cannot be rewritten.
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

CREATE TABLE service_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    category_id INTEGER NOT NULL REFERENCES service_categories(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    provider TEXT,
    identifier TEXT,
    management_url TEXT,
    notes TEXT,
    status TEXT NOT NULL CHECK (status IN ('active','archived')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX service_items_user_id_id ON service_items(user_id,id);

-- A category must be visible to the owner and cannot change kind.
CREATE TRIGGER service_items_category_insert BEFORE INSERT ON service_items
WHEN NOT EXISTS (
    SELECT 1 FROM service_categories c WHERE c.id = NEW.category_id
        AND (c.user_id IS NULL OR c.user_id = NEW.user_id)
)
BEGIN
    SELECT RAISE(ABORT, 'category is required and must be visible');
END;
CREATE TRIGGER service_items_category_update BEFORE UPDATE OF category_id,user_id ON service_items
WHEN NOT EXISTS (
    SELECT 1 FROM service_categories c WHERE c.id = NEW.category_id
        AND (c.user_id IS NULL OR c.user_id = NEW.user_id)
) OR (SELECT kind FROM service_categories WHERE id = OLD.category_id)
    IS NOT (SELECT kind FROM service_categories WHERE id = NEW.category_id)
BEGIN
    SELECT RAISE(ABORT, 'category must be visible and retain its kind');
END;

-- WITHOUT ROWID keeps the parent ID as the real primary key: an INTEGER
-- PRIMARY KEY in a rowid table is a rowid alias that silently accepts NULL.
CREATE TABLE sim_details (
    service_id INTEGER PRIMARY KEY REFERENCES service_items(id) ON DELETE CASCADE,
    card_type TEXT NOT NULL CHECK (card_type IN ('physical_sim','esim')),
    phone_number TEXT,
    country_or_region TEXT,
    iccid TEXT
) WITHOUT ROWID;

CREATE TABLE service_periods (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_id INTEGER NOT NULL REFERENCES service_items(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    starts_at INTEGER,
    expires_at INTEGER NOT NULL,
    date_precision TEXT NOT NULL CHECK (date_precision IN ('date','datetime')),
    local_expires_date TEXT,
    timezone TEXT NOT NULL,
    cycle INTEGER NOT NULL DEFAULT 1 CHECK (cycle >= 1),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (
        (date_precision = 'date' AND local_expires_date IS NOT NULL
            AND local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR
        (date_precision = 'datetime' AND local_expires_date IS NULL)
    )
);
CREATE INDEX service_periods_service_id_id ON service_periods(service_id,id);
CREATE INDEX service_periods_expires_at ON service_periods(expires_at);

CREATE TABLE period_terms (
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
            AND interval_unit IS NOT NULL AND interval_unit IN ('day','week','month','year')) OR
        (mode IN ('once','custom') AND interval_count IS NULL AND interval_unit IS NULL)
    ),
    CHECK (
        (amount IS NULL AND currency IS NULL) OR
        (amount IS NOT NULL AND typeof(amount) = 'integer' AND amount >= 0
            AND currency IS NOT NULL AND currency GLOB '[A-Z][A-Z][A-Z]')
    )
) WITHOUT ROWID;

CREATE TABLE reminder_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    period_id INTEGER NOT NULL REFERENCES service_periods(id) ON DELETE CASCADE,
    days_before INTEGER NOT NULL CHECK (days_before >= 0),
    reminder_time TEXT NOT NULL CHECK (reminder_time GLOB '[0-2][0-9]:[0-5][0-9]' AND reminder_time < '24:00'),
    enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(period_id,days_before,reminder_time),
    UNIQUE(id,period_id)
);
CREATE INDEX reminder_rules_period_id_id ON reminder_rules(period_id,id);

CREATE TABLE renewals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    period_id INTEGER NOT NULL REFERENCES service_periods(id) ON DELETE CASCADE,
    previous_cycle INTEGER NOT NULL CHECK (previous_cycle >= 1),
    previous_expires_at INTEGER NOT NULL,
    new_expires_at INTEGER NOT NULL,
    previous_date_precision TEXT NOT NULL CHECK (previous_date_precision IN ('date','datetime')),
    new_date_precision TEXT NOT NULL CHECK (new_date_precision IN ('date','datetime')),
    previous_local_expires_date TEXT,
    new_local_expires_date TEXT,
    timezone TEXT NOT NULL,
    renewed_at INTEGER NOT NULL,
    amount INTEGER CHECK (amount >= 0),
    currency TEXT,
    notes TEXT,
    created_at INTEGER NOT NULL,
    UNIQUE(period_id,previous_cycle),
    CHECK ((amount IS NULL AND currency IS NULL) OR (amount IS NOT NULL AND currency IS NOT NULL AND currency GLOB '[A-Z][A-Z][A-Z]')),
    CHECK ((previous_date_precision = 'date' AND previous_local_expires_date IS NOT NULL AND previous_local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR (previous_date_precision = 'datetime' AND previous_local_expires_date IS NULL)),
    CHECK ((new_date_precision = 'date' AND new_local_expires_date IS NOT NULL AND new_local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR (new_date_precision = 'datetime' AND new_local_expires_date IS NULL))
);

CREATE TABLE reminder_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    period_id INTEGER NOT NULL REFERENCES service_periods(id) ON DELETE CASCADE,
    reminder_rule_id INTEGER NOT NULL REFERENCES reminder_rules(id) ON DELETE CASCADE,
    cycle INTEGER NOT NULL CHECK (cycle >= 1),
    rule_revision INTEGER NOT NULL CHECK (rule_revision >= 1),
    scheduled_at INTEGER NOT NULL,
    expires_at_snapshot INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','sending','sent','failed','cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at INTEGER,
    locked_until INTEGER,
    attempt_token TEXT,
    sent_at INTEGER,
    telegram_message_id INTEGER,
    last_error_code TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(period_id,reminder_rule_id,cycle,rule_revision),
    FOREIGN KEY (reminder_rule_id,period_id) REFERENCES reminder_rules(id,period_id) ON DELETE CASCADE
);
CREATE INDEX reminder_jobs_status_next_attempt_id ON reminder_jobs(status,next_attempt_at,id);
CREATE INDEX reminder_jobs_status_locked_until ON reminder_jobs(status,locked_until);
CREATE INDEX reminder_jobs_delivery_identity ON reminder_jobs(period_id,reminder_rule_id,expires_at_snapshot);

CREATE TABLE bot_dialogs (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    chat_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK (action IN (
        'renew','service_add','service_edit','period_add',
        'period_edit','rule_add','rule_edit','timezone'
    )),
    step TEXT NOT NULL CHECK (step = trim(step) AND length(step) BETWEEN 1 AND 64),
    draft_json TEXT NOT NULL CHECK (length(CAST(draft_json AS BLOB)) <= 16384),
    service_id INTEGER REFERENCES service_items(id) ON DELETE CASCADE,
    period_id INTEGER REFERENCES service_periods(id) ON DELETE CASCADE,
    rule_id INTEGER REFERENCES reminder_rules(id) ON DELETE CASCADE,
    expected_cycle INTEGER CHECK (expected_cycle IS NULL OR expected_cycle >= 1),
    expected_terms_revision INTEGER CHECK (expected_terms_revision IS NULL OR expected_terms_revision >= 1),
    expected_revision INTEGER CHECK (expected_revision IS NULL OR expected_revision >= 1),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX bot_dialogs_period_id ON bot_dialogs(period_id);
CREATE INDEX bot_dialogs_service_id ON bot_dialogs(service_id);
CREATE INDEX bot_dialogs_rule_id ON bot_dialogs(rule_id);
