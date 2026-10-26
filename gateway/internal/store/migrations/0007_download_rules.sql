-- Download rules: who may download ('anyone' with the link, or signed-in 'members'), and how
-- many times a link may be downloaded (NULL: no limit).
ALTER TABLE org_rules ADD COLUMN download_access TEXT NOT NULL DEFAULT 'anyone' CHECK (download_access IN ('anyone', 'members'));
ALTER TABLE org_rules ADD COLUMN max_downloads INTEGER;
