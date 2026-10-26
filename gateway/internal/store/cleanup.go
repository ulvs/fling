package store

import (
	"context"
	"time"
)

// FilesToDelete returns files still in storage that nobody can download any more:
//   - shared files whose every link ended (expired, or was turned off) before linkEndedBefore,
//   - uploads that were started before startedBefore and never completed.
func (s *Store) FilesToDelete(ctx context.Context, linkEndedBefore, startedBefore time.Time, limit int) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.org_id, f.owner_id, f.object_key, f.name, f.size, f.state
		FROM files f
		WHERE f.deleted_at IS NULL AND (
			(f.state = 'complete' AND NOT EXISTS (
				SELECT 1 FROM links l WHERE l.file_id = f.id
				AND (CASE WHEN l.revoked_at IS NOT NULL AND l.revoked_at < l.expires_at THEN l.revoked_at ELSE l.expires_at END) > ?))
			OR (f.state = 'pending' AND f.created_at < ?))
		ORDER BY f.created_at
		LIMIT ?`, linkEndedBefore.Unix(), startedBefore.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.OrgID, &f.OwnerID, &f.ObjectKey, &f.Name, &f.Size, &f.State); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) MarkFileDeleted(ctx context.Context, fileID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET deleted_at = ? WHERE id = ?`, now.Unix(), fileID)
	return err
}
