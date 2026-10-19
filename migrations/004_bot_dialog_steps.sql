-- Generalize the single renewal dialog row into a durable, multi-step wizard.
-- Existing renewal rows are preserved verbatim: action=renew and the renewal
-- step name; the draft starts empty. A row whose action cannot be mapped aborts
-- the whole migration (CASE yields NULL against NOT NULL) instead of being
-- dropped or rewritten.
CREATE TABLE bot_dialogs_new (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    chat_id INTEGER NOT NULL,
    action TEXT NOT NULL CHECK (action IN (
        'renew','service_add','service_edit','period_add',
        'terms_edit','rule_add','rule_edit','timezone'
    )),
    step TEXT NOT NULL CHECK (step = trim(step) AND length(step) BETWEEN 1 AND 64),
    draft_json TEXT NOT NULL CHECK (length(CAST(draft_json AS BLOB)) <= 16384),
    service_id INTEGER REFERENCES service_items(id) ON DELETE CASCADE,
    validity_period_id INTEGER REFERENCES validity_periods(id) ON DELETE CASCADE,
    rule_id INTEGER REFERENCES reminder_rules(id) ON DELETE CASCADE,
    expected_cycle INTEGER CHECK (expected_cycle IS NULL OR expected_cycle >= 1),
    expected_terms_revision INTEGER CHECK (expected_terms_revision IS NULL OR expected_terms_revision >= 1),
    expected_revision INTEGER CHECK (expected_revision IS NULL OR expected_revision >= 1),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
INSERT INTO bot_dialogs_new(user_id,chat_id,action,step,draft_json,validity_period_id,expected_cycle,expires_at,created_at)
    SELECT user_id,chat_id,
        CASE WHEN action = 'renew' THEN 'renew' END,
        'await_expiration','{}',validity_period_id,expected_cycle,expires_at,created_at
    FROM bot_dialogs;
DROP TABLE bot_dialogs;
ALTER TABLE bot_dialogs_new RENAME TO bot_dialogs;
CREATE INDEX bot_dialogs_validity_period_id ON bot_dialogs(validity_period_id);
CREATE INDEX bot_dialogs_service_id ON bot_dialogs(service_id);
CREATE INDEX bot_dialogs_rule_id ON bot_dialogs(rule_id);
