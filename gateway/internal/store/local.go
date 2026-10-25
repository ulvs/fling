package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Account is a user with the details local sign-in needs.
type Account struct {
	User
	PasswordHash string
	TOTPSecret   string
	TOTPPending  string
	TOTPLastStep int64
	WebAuthnID   []byte
	LastLogin    *time.Time
	Passkeys     int
}

const accountColumns = `u.id, u.org_id, u.email, u.local, u.disabled_at IS NOT NULL, u.is_admin, COALESCE(u.password_hash, ''),
	COALESCE(u.totp_secret, ''), COALESCE(u.totp_pending, ''), u.totp_last_step, u.webauthn_id, u.last_login_at,
	(SELECT COUNT(*) FROM passkeys p WHERE p.user_id = u.id)`

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var lastLogin sql.NullInt64
	err := row.Scan(&a.ID, &a.OrgID, &a.Email, &a.Local, &a.Disabled, &a.Admin, &a.PasswordHash, &a.TOTPSecret, &a.TOTPPending,
		&a.TOTPLastStep, &a.WebAuthnID, &lastLogin, &a.Passkeys)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0)
		a.LastLogin = &t
	}
	return a, err
}

func (s *Store) AccountByEmail(ctx context.Context, orgID, email string) (Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM users u WHERE u.org_id = ? AND u.email = ?`,
		orgID, strings.ToLower(strings.TrimSpace(email))))
}

func (s *Store) AccountByID(ctx context.Context, id string) (Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM users u WHERE u.id = ?`, id))
}

func (s *Store) AccountByWebAuthnID(ctx context.Context, handle []byte) (Account, error) {
	return scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM users u WHERE u.webauthn_id = ?`, handle))
}

func (s *Store) ListAccounts(ctx context.Context, orgID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM users u WHERE u.org_id = ? ORDER BY u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// EnsureLocalUser creates the user if needed and marks it a local account.
func (s *Store) EnsureLocalUser(ctx context.Context, orgID, email string, now time.Time) (User, error) {
	u, err := s.EnsureUser(ctx, orgID, email, now)
	if err != nil {
		return u, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET local = 1 WHERE id = ?`, u.ID); err != nil {
		return u, err
	}
	u.Local = true
	return u, nil
}

// EnsureWebAuthnID returns the user's passkey handle, creating a random one the first time.
func (s *Store) EnsureWebAuthnID(ctx context.Context, userID string) ([]byte, error) {
	handle := make([]byte, 32)
	rand.Read(handle)
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET webauthn_id = ? WHERE id = ? AND webauthn_id IS NULL`, handle, userID); err != nil {
		return nil, err
	}
	var out []byte
	err := s.db.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id = ?`, userID).Scan(&out)
	return out, err
}

// SetDisabled turns an account off (ending all its sessions) or back on.
func (s *Store) SetDisabled(ctx context.Context, userID string, disabled bool, now time.Time) error {
	if !disabled {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = NULL WHERE id = ?`, userID)
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled_at = COALESCE(disabled_at, ?) WHERE id = ?`, now.Unix(), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = NULLIF(?, '') WHERE id = ?`, hash, userID)
	return err
}

// DeleteSessions ends the user's sessions except the one with keepHash (may be empty).
func (s *Store) DeleteSessions(ctx context.Context, userID, keepHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepHash)
	return err
}

// ---- two-step codes -------------------------------------------------------------

func (s *Store) SetTOTPPending(ctx context.Context, userID, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_pending = ? WHERE id = ?`, secret, userID)
	return err
}

// EnableTOTP makes the pending secret active and stores new recovery code hashes.
func (s *Store) EnableTOTP(ctx context.Context, userID string, step int64, recoveryHashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_secret = totp_pending, totp_pending = NULL, totp_last_step = ? WHERE id = ? AND totp_pending IS NOT NULL`,
		step, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	if err := replaceRecoveryCodes(ctx, tx, userID, recoveryHashes); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DisableTOTP(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_pending = NULL WHERE id = ?`, userID); err != nil {
		return err
	}
	if err := replaceRecoveryCodes(ctx, tx, userID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// UseTOTPStep records step as used. It fails (ErrNotFound) if an equal or later step was
// already used, which stops the same code being accepted twice even under concurrency.
func (s *Store) UseTOTPStep(ctx context.Context, userID string, step int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, userID, step)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID string, hashes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h); err != nil {
			return err
		}
	}
	return nil
}

// UseRecoveryCode marks an unused recovery code as used; ErrNotFound if there's none.
func (s *Store) UseRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, now.Unix(), userID, codeHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecoveryCodesLeft(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

// ---- invite and reset links ---------------------------------------------------------

type AccountLink struct {
	UserID    string
	Purpose   string
	ExpiresAt time.Time
}

func (s *Store) CreateAccountLink(ctx context.Context, tokenHash, userID, purpose string, expires, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO account_links (token_hash, user_id, purpose, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, purpose, now.Unix(), expires.Unix())
	return err
}

func (s *Store) AccountLinkByHash(ctx context.Context, tokenHash string, now time.Time) (AccountLink, error) {
	var l AccountLink
	var expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, purpose, expires_at FROM account_links WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).
		Scan(&l.UserID, &l.Purpose, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrNotFound
	}
	l.ExpiresAt = time.Unix(expires, 0)
	return l, err
}

// UseAccountLink deletes the link and every other open link of the same user.
func (s *Store) UseAccountLink(ctx context.Context, tokenHash string, now time.Time) error {
	var userID string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM account_links WHERE token_hash = ? AND expires_at > ? RETURNING user_id`, tokenHash, now.Unix()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM account_links WHERE user_id = ?`, userID)
	return err
}

// ---- passkeys -----------------------------------------------------------------------

type Passkey struct {
	ID         string
	UserID     string
	Name       string
	Credential string // JSON
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys (id, user_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.UserID, p.Name, p.Credential, p.CreatedAt.Unix())
	return err
}

func (s *Store) Passkeys(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, credential, created_at, last_used_at FROM passkeys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var created int64
		var used sql.NullInt64
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Credential, &created, &used); err != nil {
			return nil, err
		}
		p.CreatedAt = time.Unix(created, 0)
		if used.Valid {
			t := time.Unix(used.Int64, 0)
			p.LastUsedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePasskeyUse stores the credential after a sign-in (its signature counter changes).
func (s *Store) UpdatePasskeyUse(ctx context.Context, id, credential string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`, credential, now.Unix(), id)
	return err
}

func (s *Store) DeletePasskey(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// ---- sign-in steps ----------------------------------------------------------------

type AuthStep struct {
	Purpose     string
	UserID      string
	Data        string
	DesktopFlow string
	Attempts    int
}

func (s *Store) CreateAuthStep(ctx context.Context, tokenHash string, st AuthStep, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_steps (token_hash, purpose, user_id, data, desktop_flow, expires_at) VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)`,
		tokenHash, st.Purpose, st.UserID, st.Data, st.DesktopFlow, expires.Unix())
	return err
}

// AuthStepByHash returns an unexpired step with the given purpose.
func (s *Store) AuthStepByHash(ctx context.Context, tokenHash, purpose string, now time.Time) (AuthStep, error) {
	var st AuthStep
	var user sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT purpose, user_id, data, desktop_flow, attempts FROM auth_steps WHERE token_hash = ? AND purpose = ? AND expires_at > ?`,
		tokenHash, purpose, now.Unix()).
		Scan(&st.Purpose, &user, &st.Data, &st.DesktopFlow, &st.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	st.UserID = user.String
	return st, err
}

// TakeAuthStep returns and deletes an unexpired step, so it works once.
func (s *Store) TakeAuthStep(ctx context.Context, tokenHash, purpose string, now time.Time) (AuthStep, error) {
	var st AuthStep
	var user sql.NullString
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM auth_steps WHERE token_hash = ? AND purpose = ? AND expires_at > ?
		 RETURNING purpose, user_id, data, desktop_flow, attempts`, tokenHash, purpose, now.Unix()).
		Scan(&st.Purpose, &user, &st.Data, &st.DesktopFlow, &st.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	st.UserID = user.String
	return st, err
}

// FailAuthStep counts a wrong code and deletes the step once maxAttempts is reached.
func (s *Store) FailAuthStep(ctx context.Context, tokenHash string, maxAttempts int) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE auth_steps SET attempts = attempts + 1 WHERE token_hash = ?`, tokenHash); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_steps WHERE token_hash = ? AND attempts >= ?`, tokenHash, maxAttempts)
	return err
}

func (s *Store) UpdateAuthStepData(ctx context.Context, tokenHash, data string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_steps SET data = ? WHERE token_hash = ?`, data, tokenHash)
	return err
}
