-- Rules an admin sets on the Rules page. A NULL limit means "use the server's setting"
-- (FLING_DEFAULT_LINK_TTL, FLING_MAX_LINK_TTL, FLING_MAX_UPLOAD_BYTES).
CREATE TABLE org_rules (
    org_id              TEXT PRIMARY KEY REFERENCES orgs(id),
    default_ttl_seconds INTEGER,
    max_ttl_seconds     INTEGER,
    max_upload_bytes    INTEGER,
    blocked_types       TEXT NOT NULL DEFAULT '',
    allowed_types       TEXT NOT NULL DEFAULT '',
    updated_at          INTEGER NOT NULL,
    updated_by          TEXT
);
