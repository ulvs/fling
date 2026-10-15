// Package store persists organisations, users, files, links and the audit log.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type User struct {
	ID    string
	OrgID string
	Email string
}

type File struct {
	ID          string
	OrgID       string
	OwnerID     string
	ObjectKey   string
	Name        string
	Size        int64
	ContentType string
	LinkTTL     time.Duration
	State       string
	CreatedAt   time.Time
}

type Link struct {
	ID            string
	OrgID         string
	FileID        string
	Slug          string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	DownloadCount int64
	CreatedAt     time.Time
}

// LinkWithFile is a link joined with the file it shares.
type LinkWithFile struct {
	Link
	File File
}

type AuditEvent struct {
	OrgID   string
	At      time.Time
	ActorID string
	Action  string
	Target  string
	Detail  string
}

// Open opens (creating if needed) the SQLite database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	dsn := "file:" + path + sep + "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite serialises writers; one connection avoids SQLITE_BUSY and keeps in-memory test DBs whole.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(name, "migrations/"), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", name)
		}
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// NewID returns a random 128-bit identifier as hex.
func NewID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSlug returns a random 128-bit, URL-safe link slug.
func NewSlug() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Store) EnsureOrg(ctx context.Context, id, name string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO orgs (id, name, created_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING`,
		id, name, now.Unix())
	return err
}

func (s *Store) EnsureUser(ctx context.Context, orgID, email string, now time.Time) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, org_id, email, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (org_id, email) DO NOTHING`,
		NewID(), orgID, email, now.Unix())
	if err != nil {
		return User{}, err
	}
	u := User{OrgID: orgID, Email: email}
	err = s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE org_id = ? AND email = ?`, orgID, email).Scan(&u.ID)
	return u, err
}

func (s *Store) CreateFile(ctx context.Context, f File) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO files (id, org_id, owner_id, object_key, name, size, content_type, link_ttl_seconds, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		f.ID, f.OrgID, f.OwnerID, f.ObjectKey, f.Name, f.Size, f.ContentType, int64(f.LinkTTL/time.Second), f.CreatedAt.Unix())
	return err
}

// GetFile returns a file owned by ownerID in orgID.
func (s *Store) GetFile(ctx context.Context, orgID, ownerID, id string) (File, error) {
	var f File
	var ttl, created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, owner_id, object_key, name, size, content_type, link_ttl_seconds, state, created_at
		 FROM files WHERE id = ? AND org_id = ? AND owner_id = ?`, id, orgID, ownerID).
		Scan(&f.ID, &f.OrgID, &f.OwnerID, &f.ObjectKey, &f.Name, &f.Size, &f.ContentType, &ttl, &f.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	f.LinkTTL = time.Duration(ttl) * time.Second
	f.CreatedAt = time.Unix(created, 0)
	return f, err
}

// CompleteFile marks a pending file complete and creates its link in one transaction.
func (s *Store) CompleteFile(ctx context.Context, fileID string, link Link, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE files SET state = 'complete', completed_at = ? WHERE id = ? AND state = 'pending'`, now.Unix(), fileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO links (id, org_id, file_id, slug, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		link.ID, link.OrgID, fileID, link.Slug, link.ExpiresAt.Unix(), link.CreatedAt.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

const linkColumns = `l.id, l.org_id, l.file_id, l.slug, l.expires_at, l.revoked_at, l.download_count, l.created_at,
	f.id, f.org_id, f.owner_id, f.object_key, f.name, f.size, f.content_type, f.created_at`

func scanLink(row interface{ Scan(...any) error }) (LinkWithFile, error) {
	var lf LinkWithFile
	var expires, created, fileCreated int64
	var revoked sql.NullInt64
	err := row.Scan(&lf.ID, &lf.OrgID, &lf.FileID, &lf.Slug, &expires, &revoked, &lf.DownloadCount, &created,
		&lf.File.ID, &lf.File.OrgID, &lf.File.OwnerID, &lf.File.ObjectKey, &lf.File.Name, &lf.File.Size, &lf.File.ContentType, &fileCreated)
	if err != nil {
		return lf, err
	}
	lf.ExpiresAt = time.Unix(expires, 0)
	lf.CreatedAt = time.Unix(created, 0)
	lf.File.CreatedAt = time.Unix(fileCreated, 0)
	if revoked.Valid {
		t := time.Unix(revoked.Int64, 0)
		lf.RevokedAt = &t
	}
	return lf, nil
}

func (s *Store) LinkBySlug(ctx context.Context, slug string) (LinkWithFile, error) {
	lf, err := scanLink(s.db.QueryRowContext(ctx,
		`SELECT `+linkColumns+` FROM links l JOIN files f ON f.id = l.file_id WHERE l.slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return lf, ErrNotFound
	}
	return lf, err
}

func (s *Store) ListLinks(ctx context.Context, orgID, ownerID string) ([]LinkWithFile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+linkColumns+` FROM links l JOIN files f ON f.id = l.file_id
		 WHERE l.org_id = ? AND f.owner_id = ? ORDER BY l.created_at DESC, l.id`, orgID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinkWithFile
	for rows.Next() {
		lf, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lf)
	}
	return out, rows.Err()
}

// RevokeLink revokes a link owned by ownerID. Revoking twice is not an error.
func (s *Store) RevokeLink(ctx context.Context, orgID, ownerID, linkID string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE links SET revoked_at = COALESCE(revoked_at, ?)
		 WHERE id = ? AND org_id = ? AND file_id IN (SELECT id FROM files WHERE owner_id = ?)`,
		now.Unix(), linkID, orgID, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountDownload(ctx context.Context, linkID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE links SET download_count = download_count + 1 WHERE id = ?`, linkID)
	return err
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events (org_id, at, actor_id, action, target, detail) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)`,
		e.OrgID, e.At.Unix(), e.ActorID, e.Action, e.Target, e.Detail)
	return err
}
