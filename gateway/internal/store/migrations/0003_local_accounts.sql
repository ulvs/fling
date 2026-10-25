-- Local accounts: users the gateway manages itself (invited by an admin), signing in with a
-- passkey, or a password plus an optional two-step code.
ALTER TABLE users ADD COLUMN local INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN disabled_at INTEGER;
ALTER TABLE users ADD COLUMN password_hash TEXT;      -- Argon2id, PHC format
ALTER TABLE users ADD COLUMN totp_secret TEXT;        -- set once two-step codes are on
ALTER TABLE users ADD COLUMN totp_pending TEXT;       -- during setup, until the first code checks out
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0; -- blocks code replay
ALTER TABLE users ADD COLUMN webauthn_id BLOB;        -- random passkey user handle

-- One-time invite and password-reset links.
CREATE TABLE account_links (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id),
    purpose    TEXT NOT NULL CHECK (purpose IN ('invite', 'reset')),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE recovery_codes (
    user_id   TEXT NOT NULL REFERENCES users(id),
    code_hash TEXT NOT NULL,
    used_at   INTEGER,
    PRIMARY KEY (user_id, code_hash)
);

CREATE TABLE passkeys (
    id           TEXT PRIMARY KEY,     -- credential ID, base64url
    user_id      TEXT NOT NULL REFERENCES users(id),
    name         TEXT NOT NULL,
    credential   TEXT NOT NULL,        -- the go-webauthn Credential as JSON
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);
CREATE INDEX passkeys_user ON passkeys (user_id);

-- A sign-in that's half done: password accepted but a two-step code still due, or a passkey
-- ceremony waiting for the browser. Bound to the browser by a cookie holding the token.
CREATE TABLE auth_steps (
    token_hash   TEXT PRIMARY KEY,
    purpose      TEXT NOT NULL,
    user_id      TEXT REFERENCES users(id),
    data         TEXT NOT NULL DEFAULT '',
    desktop_flow TEXT NOT NULL DEFAULT '',
    attempts     INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER NOT NULL
);
