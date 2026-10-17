CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    telegram_user_id INTEGER NOT NULL UNIQUE CHECK (telegram_user_id > 0),
    timezone TEXT NOT NULL,
    private_chat_id INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE sims (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    card_type TEXT NOT NULL CHECK (card_type IN ('physical_sim','esim')),
    operator TEXT,
    phone_number TEXT,
    country_or_region TEXT,
    iccid TEXT,
    notes TEXT,
    status TEXT NOT NULL CHECK (status IN ('active','archived')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX sims_user_id_id ON sims(user_id,id);
CREATE TABLE validity_periods (
    id INTEGER PRIMARY KEY,
    sim_id INTEGER NOT NULL REFERENCES sims(id) ON DELETE RESTRICT,
    period_type TEXT NOT NULL CHECK (period_type IN ('number_validity','plan_validity','data_package_validity','custom')),
    name TEXT NOT NULL,
    starts_at INTEGER,
    expires_at INTEGER NOT NULL,
    date_precision TEXT NOT NULL CHECK (date_precision IN ('date','datetime')),
    local_expires_date TEXT,
    timezone TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','expired','cancelled')),
    cycle INTEGER NOT NULL DEFAULT 1 CHECK (cycle >= 1),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK ((date_precision = 'date' AND local_expires_date IS NOT NULL AND local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR (date_precision = 'datetime' AND local_expires_date IS NULL))
);
CREATE INDEX validity_periods_sim_id_id ON validity_periods(sim_id,id);
CREATE INDEX validity_periods_status_expires_at ON validity_periods(status,expires_at);
CREATE TABLE reminder_rules (
    id INTEGER PRIMARY KEY,
    validity_period_id INTEGER NOT NULL REFERENCES validity_periods(id) ON DELETE RESTRICT,
    days_before INTEGER NOT NULL CHECK (days_before >= 0),
    reminder_time TEXT NOT NULL CHECK (reminder_time GLOB '[0-2][0-9]:[0-5][0-9]' AND reminder_time < '24:00'),
    enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(validity_period_id,days_before,reminder_time),
    UNIQUE(id,validity_period_id)
);
CREATE TABLE renewals (
    id INTEGER PRIMARY KEY,
    validity_period_id INTEGER NOT NULL REFERENCES validity_periods(id) ON DELETE RESTRICT,
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
    UNIQUE(validity_period_id,previous_cycle),
    CHECK ((amount IS NULL AND currency IS NULL) OR (amount IS NOT NULL AND currency IS NOT NULL AND currency GLOB '[A-Z][A-Z][A-Z]')),
    CHECK ((previous_date_precision = 'date' AND previous_local_expires_date IS NOT NULL AND previous_local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR (previous_date_precision = 'datetime' AND previous_local_expires_date IS NULL)),
    CHECK ((new_date_precision = 'date' AND new_local_expires_date IS NOT NULL AND new_local_expires_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]') OR (new_date_precision = 'datetime' AND new_local_expires_date IS NULL))
);
CREATE TABLE reminder_jobs (
    id INTEGER PRIMARY KEY,
    validity_period_id INTEGER NOT NULL REFERENCES validity_periods(id) ON DELETE RESTRICT,
    reminder_rule_id INTEGER NOT NULL REFERENCES reminder_rules(id) ON DELETE RESTRICT,
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
    UNIQUE(validity_period_id,reminder_rule_id,cycle,rule_revision),
    FOREIGN KEY (reminder_rule_id,validity_period_id) REFERENCES reminder_rules(id,validity_period_id) ON DELETE RESTRICT
);
CREATE INDEX reminder_jobs_status_next_attempt_id ON reminder_jobs(status,next_attempt_at,id);
CREATE INDEX reminder_jobs_status_locked_until ON reminder_jobs(status,locked_until);
CREATE INDEX reminder_jobs_delivery_identity ON reminder_jobs(validity_period_id,reminder_rule_id,expires_at_snapshot);
CREATE TABLE bot_dialogs (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    chat_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK (action = 'renew'),
    validity_period_id INTEGER NOT NULL REFERENCES validity_periods(id) ON DELETE RESTRICT,
    expected_cycle INTEGER NOT NULL CHECK (expected_cycle >= 1),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
