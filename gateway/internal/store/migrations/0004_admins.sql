-- Admins manage people and links on the gateway's admin pages. FLING_ADMIN_EMAILS also makes
-- someone an admin without this flag, so the first admin needs no database access.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

CREATE INDEX links_org_created ON links (org_id, created_at);
