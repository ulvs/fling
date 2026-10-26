-- Files are deleted from storage some time after their last link ended; the row stays for
-- the history and the activity log.
ALTER TABLE files ADD COLUMN deleted_at INTEGER;
CREATE INDEX files_cleanup ON files (deleted_at, state, created_at);
