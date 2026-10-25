package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

// NewToken returns a random 256-bit secret for cookies, desktop tokens and one-time codes.
func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is how tokens are stored and looked up.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type LoginState struct {
	State       string
	Provider    string
	Nonce       string
	Verifier    string
	ReturnTo    string
	DesktopFlow string
	ExpiresAt   time.Time
}

type DesktopFlow struct {
	ID            string
	RedirectURI   string
	ClientState   string
	CodeChallenge string
	ExpiresAt     time.Time
	UserID        string
	OrgID         string
}

func (s *Store) CreateLoginState(ctx context.Context, l LoginState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_states (state, provider, nonce, verifier, return_to, desktop_flow, expires_at)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?)`,
		l.State, l.Provider, l.Nonce, l.Verifier, l.ReturnTo, l.DesktopFlow, l.ExpiresAt.Unix())
	return err
}

// TakeLoginState returns and deletes an unexpired login state, so each one works once.
func (s *Store) TakeLoginState(ctx context.Context, state string, now time.Time) (LoginState, error) {
	var l LoginState
	var flow sql.NullString
	var expires int64
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM login_states WHERE state = ? AND expires_at > ?
		 RETURNING state, provider, nonce, verifier, return_to, desktop_flow, expires_at`, state, now.Unix()).
		Scan(&l.State, &l.Provider, &l.Nonce, &l.Verifier, &l.ReturnTo, &flow, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	l.DesktopFlow = flow.String
	l.ExpiresAt = time.Unix(expires, 0)
	return l, err
}

func (s *Store) CreateDesktopFlow(ctx context.Context, f DesktopFlow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO desktop_flows (id, redirect_uri, client_state, code_challenge, expires_at) VALUES (?, ?, ?, ?, ?)`,
		f.ID, f.RedirectURI, f.ClientState, f.CodeChallenge, f.ExpiresAt.Unix())
	return err
}

// PendingDesktopFlow returns an unexpired desktop flow that hasn't been approved yet.
func (s *Store) PendingDesktopFlow(ctx context.Context, id string, now time.Time) (DesktopFlow, error) {
	var f DesktopFlow
	var expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, redirect_uri, client_state, code_challenge, expires_at FROM desktop_flows
		 WHERE id = ? AND expires_at > ? AND code_hash IS NULL`, id, now.Unix()).
		Scan(&f.ID, &f.RedirectURI, &f.ClientState, &f.CodeChallenge, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	f.ExpiresAt = time.Unix(expires, 0)
	return f, err
}

// ApproveDesktopFlow attaches the signed-in user and a one-time code to a pending flow.
func (s *Store) ApproveDesktopFlow(ctx context.Context, id string, user User, codeHash string, codeExpires, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE desktop_flows SET code_hash = ?, user_id = ?, org_id = ?, expires_at = ?
		 WHERE id = ? AND expires_at > ? AND code_hash IS NULL`,
		codeHash, user.ID, user.OrgID, codeExpires.Unix(), id, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// TakeDesktopCode returns and deletes the approved flow for a one-time code.
func (s *Store) TakeDesktopCode(ctx context.Context, codeHash string, now time.Time) (DesktopFlow, error) {
	var f DesktopFlow
	var expires int64
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM desktop_flows WHERE code_hash = ? AND expires_at > ?
		 RETURNING id, redirect_uri, client_state, code_challenge, expires_at, user_id, org_id`, codeHash, now.Unix()).
		Scan(&f.ID, &f.RedirectURI, &f.ClientState, &f.CodeChallenge, &expires, &f.UserID, &f.OrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	f.ExpiresAt = time.Unix(expires, 0)
	return f, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, user User, kind string, expires, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, org_id, kind, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tokenHash, user.ID, user.OrgID, kind, now.Unix(), expires.Unix(), now.Unix())
	return err
}

type Session struct {
	User      User
	Kind      string
	ExpiresAt time.Time
	LastSeen  time.Time
}

// SessionByHash returns the unexpired session and its user.
func (s *Store) SessionByHash(ctx context.Context, tokenHash string, now time.Time) (Session, error) {
	var se Session
	var expires, seen int64
	err := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.org_id, u.email, u.local, u.disabled_at IS NOT NULL, s.kind, s.expires_at, s.last_seen_at
		 FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`,
		tokenHash, now.Unix()).
		Scan(&se.User.ID, &se.User.OrgID, &se.User.Email, &se.User.Local, &se.User.Disabled, &se.Kind, &expires, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return se, ErrNotFound
	}
	se.ExpiresAt = time.Unix(expires, 0)
	se.LastSeen = time.Unix(seen, 0)
	return se, err
}

// TouchSession records use and moves the expiry (sliding sessions).
func (s *Store) TouchSession(ctx context.Context, tokenHash string, expires, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = MAX(expires_at, ?) WHERE token_hash = ?`,
		now.Unix(), expires.Unix(), tokenHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) RecordLogin(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now.Unix(), userID)
	return err
}

// DeleteExpired removes expired sessions, sign-ins in progress and desktop flows.
func (s *Store) DeleteExpired(ctx context.Context, now time.Time) error {
	for _, q := range []string{
		`DELETE FROM sessions WHERE expires_at <= ?`,
		`DELETE FROM login_states WHERE expires_at <= ?`,
		`DELETE FROM desktop_flows WHERE expires_at <= ?`,
		`DELETE FROM account_links WHERE expires_at <= ?`,
		`DELETE FROM auth_steps WHERE expires_at <= ?`,
	} {
		if _, err := s.db.ExecContext(ctx, q, now.Unix()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	u := User{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT org_id, email, local, disabled_at IS NOT NULL FROM users WHERE id = ?`, id).
		Scan(&u.OrgID, &u.Email, &u.Local, &u.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}
