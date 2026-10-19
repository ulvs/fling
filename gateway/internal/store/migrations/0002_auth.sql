-- Signed-in sessions: browser cookies and desktop bearer tokens. Only SHA-256 hashes of the
-- tokens are stored, so a copy of the database can't be used to sign in.
CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    org_id       TEXT NOT NULL REFERENCES orgs(id),
    kind         TEXT NOT NULL CHECK (kind IN ('browser', 'desktop')),
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);

-- A sign-in in progress at an identity provider: the OAuth state and what we need to finish it.
CREATE TABLE login_states (
    state        TEXT PRIMARY KEY,
    provider     TEXT NOT NULL,
    nonce        TEXT NOT NULL,
    verifier     TEXT NOT NULL,
    return_to    TEXT NOT NULL DEFAULT '/',
    desktop_flow TEXT,
    expires_at   INTEGER NOT NULL
);

-- A desktop app signing in through the browser (RFC 8252 loopback + PKCE). After the user
-- approves, code_hash holds the one-time code the app exchanges for a token.
CREATE TABLE desktop_flows (
    id             TEXT PRIMARY KEY,
    redirect_uri   TEXT NOT NULL,
    client_state   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at     INTEGER NOT NULL,
    code_hash      TEXT UNIQUE,
    user_id        TEXT REFERENCES users(id),
    org_id         TEXT REFERENCES orgs(id)
);

ALTER TABLE users ADD COLUMN last_login_at INTEGER;
