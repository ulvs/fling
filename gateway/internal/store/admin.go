package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Store) SetAdmin(ctx context.Context, userID string, admin bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, admin, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// AdminEmails returns the enabled accounts with the admin flag.
func (s *Store) AdminEmails(ctx context.Context, orgID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT email FROM users WHERE org_id = ? AND is_admin = 1 AND disabled_at IS NULL ORDER BY email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// OrgLink is a link with its file and the email of the person who shared it.
type OrgLink struct {
	LinkWithFile
	OwnerEmail string
}

// LinkFilter narrows ListOrgLinks. Search matches file names and owner emails.
type LinkFilter struct {
	Search     string
	ActiveOnly bool
	Limit      int
}

// ListOrgLinks lists the organisation's links, newest first.
func (s *Store) ListOrgLinks(ctx context.Context, orgID string, f LinkFilter, now time.Time) ([]OrgLink, error) {
	query := `SELECT ` + linkColumns + `, u.email FROM links l JOIN files f ON f.id = l.file_id JOIN users u ON u.id = f.owner_id
		WHERE l.org_id = ?`
	args := []any{orgID}
	if term := strings.TrimSpace(f.Search); term != "" {
		// LIKE ignores case for ASCII letters; other letters (Å, Ä, Ö) match as typed.
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(term) + "%"
		query += ` AND (f.name LIKE ? ESCAPE '\' OR u.email LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	if f.ActiveOnly {
		query += ` AND l.revoked_at IS NULL AND l.expires_at > ?`
		args = append(args, now.Unix())
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query += ` ORDER BY l.created_at DESC, l.id LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgLink
	for rows.Next() {
		var ol OrgLink
		var expires, created, fileCreated int64
		var revoked sql.NullInt64
		if err := rows.Scan(&ol.ID, &ol.OrgID, &ol.FileID, &ol.Slug, &expires, &revoked, &ol.DownloadCount, &created,
			&ol.File.ID, &ol.File.OrgID, &ol.File.OwnerID, &ol.File.ObjectKey, &ol.File.Name, &ol.File.Size, &ol.File.ContentType,
			&fileCreated, &ol.OwnerEmail); err != nil {
			return nil, err
		}
		ol.ExpiresAt, ol.CreatedAt, ol.File.CreatedAt = time.Unix(expires, 0), time.Unix(created, 0), time.Unix(fileCreated, 0)
		if revoked.Valid {
			t := time.Unix(revoked.Int64, 0)
			ol.RevokedAt = &t
		}
		out = append(out, ol)
	}
	return out, rows.Err()
}

// RevokeOrgLink turns off any link in the organisation. Revoking twice is not an error.
func (s *Store) RevokeOrgLink(ctx context.Context, orgID, linkID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE links SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ? AND org_id = ?`, now.Unix(), linkID, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AuditRow is an audit event with the actor's email and, when the target is a user, theirs.
type AuditRow struct {
	ID          int64
	At          time.Time
	ActorEmail  string
	Action      string
	Target      string
	TargetEmail string
	Detail      string
}

// ListAudit returns the newest events, older than beforeID when it's set (for paging).
func (s *Store) ListAudit(ctx context.Context, orgID string, beforeID int64, limit int) ([]AuditRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := `SELECT a.id, a.at, COALESCE(actor.email, ''), a.action, a.target, COALESCE(target.email, ''), a.detail
		FROM audit_events a
		LEFT JOIN users actor ON actor.id = a.actor_id
		LEFT JOIN users target ON target.id = a.target
		WHERE a.org_id = ?`
	args := []any{orgID}
	if beforeID > 0 {
		query += ` AND a.id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY a.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		var at int64
		if err := rows.Scan(&r.ID, &at, &r.ActorEmail, &r.Action, &r.Target, &r.TargetEmail, &r.Detail); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}
