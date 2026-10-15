CREATE TABLE orgs (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE users (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id),
    email        TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    UNIQUE (org_id, email)
);

CREATE TABLE files (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES orgs(id),
    owner_id         TEXT NOT NULL REFERENCES users(id),
    object_key       TEXT NOT NULL UNIQUE,
    name             TEXT NOT NULL,
    size             INTEGER NOT NULL,
    content_type     TEXT NOT NULL,
    link_ttl_seconds INTEGER NOT NULL,
    state            TEXT NOT NULL CHECK (state IN ('pending', 'complete')),
    created_at       INTEGER NOT NULL,
    completed_at     INTEGER
);
CREATE INDEX files_owner ON files (org_id, owner_id);

CREATE TABLE links (
    id             TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL REFERENCES orgs(id),
    file_id        TEXT NOT NULL REFERENCES files(id),
    slug           TEXT NOT NULL UNIQUE,
    expires_at     INTEGER NOT NULL,
    revoked_at     INTEGER,
    download_count INTEGER NOT NULL DEFAULT 0,
    created_at     INTEGER NOT NULL
);
CREATE INDEX links_file ON links (file_id);

CREATE TABLE audit_events (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id   TEXT NOT NULL,
    at       INTEGER NOT NULL,
    actor_id TEXT,
    action   TEXT NOT NULL,
    target   TEXT NOT NULL DEFAULT '',
    detail   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_org_time ON audit_events (org_id, at);
