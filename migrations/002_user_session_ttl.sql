-- Per-user initData freshness window. NULL means the process default
-- (WEBAPP_AUTH_MAX_AGE). Values are fixed choices exposed by the Mini App.
ALTER TABLE users ADD COLUMN session_ttl_seconds INTEGER
    CHECK (session_ttl_seconds IS NULL OR session_ttl_seconds IN (3600, 21600, 86400, 604800));
