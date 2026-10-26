package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Rules are an organisation's overrides of the server's upload rules. Nil means "use the
// server's setting".
type Rules struct {
	DefaultTTL     *time.Duration
	MaxTTL         *time.Duration
	MaxUploadBytes *int64
	BlockedTypes   string // comma-separated file endings, e.g. ".exe, .bat"
	AllowedTypes   string
	UpdatedAt      time.Time
	UpdatedBy      string // email
}

// OrgRules returns the organisation's rules; found is false when none were ever saved.
func (s *Store) OrgRules(ctx context.Context, orgID string) (r Rules, found bool, err error) {
	var def, max, size sql.NullInt64
	var updated int64
	var by sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT r.default_ttl_seconds, r.max_ttl_seconds, r.max_upload_bytes, r.blocked_types, r.allowed_types, r.updated_at, u.email
		 FROM org_rules r LEFT JOIN users u ON u.id = r.updated_by WHERE r.org_id = ?`, orgID).
		Scan(&def, &max, &size, &r.BlockedTypes, &r.AllowedTypes, &updated, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if def.Valid {
		d := time.Duration(def.Int64) * time.Second
		r.DefaultTTL = &d
	}
	if max.Valid {
		d := time.Duration(max.Int64) * time.Second
		r.MaxTTL = &d
	}
	if size.Valid {
		r.MaxUploadBytes = &size.Int64
	}
	r.UpdatedAt, r.UpdatedBy = time.Unix(updated, 0), by.String
	return r, true, nil
}

func (s *Store) SaveOrgRules(ctx context.Context, orgID string, r Rules, byUserID string, now time.Time) error {
	seconds := func(d *time.Duration) any {
		if d == nil {
			return nil
		}
		return int64(*d / time.Second)
	}
	var size any
	if r.MaxUploadBytes != nil {
		size = *r.MaxUploadBytes
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_rules (org_id, default_ttl_seconds, max_ttl_seconds, max_upload_bytes, blocked_types, allowed_types, updated_at, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (org_id) DO UPDATE SET default_ttl_seconds = excluded.default_ttl_seconds, max_ttl_seconds = excluded.max_ttl_seconds,
		   max_upload_bytes = excluded.max_upload_bytes, blocked_types = excluded.blocked_types, allowed_types = excluded.allowed_types,
		   updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		orgID, seconds(r.DefaultTTL), seconds(r.MaxTTL), size, r.BlockedTypes, r.AllowedTypes, now.Unix(), byUserID)
	return err
}

// ResetOrgRules goes back to the server's settings.
func (s *Store) ResetOrgRules(ctx context.Context, orgID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM org_rules WHERE org_id = ?`, orgID)
	return err
}
