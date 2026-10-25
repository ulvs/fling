package api

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ulvs/fling/gateway/internal/store"
)

// passkeyUser adapts an account to go-webauthn.
type passkeyUser struct {
	acct  store.Account
	creds []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.acct.WebAuthnID }
func (u *passkeyUser) WebAuthnName() string                       { return u.acct.Email }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.acct.Email }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Server) loadPasskeyUser(r *http.Request, acct store.Account) (*passkeyUser, error) {
	keys, err := s.Store.Passkeys(r.Context(), acct.ID)
	if err != nil {
		return nil, err
	}
	u := &passkeyUser{acct: acct}
	for _, k := range keys {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(k.Credential), &c); err != nil {
			return nil, err
		}
		u.creds = append(u.creds, c)
	}
	return u, nil
}

// passkeyCeremony is what the server keeps between "begin" and "finish".
type passkeyCeremony struct {
	Session webauthn.SessionData `json:"session"`
	Link    string               `json:"link,omitempty"` // registering from an invite or reset link
}

func jsonError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// passkeyRequest checks what every passkey call needs: passkeys on, a same-origin JSON call.
func (s *Server) passkeyRequest(w http.ResponseWriter, r *http.Request) bool {
	if s.WebAuthn == nil {
		jsonError(w, http.StatusNotFound, "Passkeys aren't turned on.")
		return false
	}
	if !s.sameOrigin(r) {
		jsonError(w, http.StatusForbidden, "Blocked.")
		return false
	}
	return true
}

func (s *Server) saveCeremony(w http.ResponseWriter, r *http.Request, purpose, userID, flow string, c passkeyCeremony) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	token := store.NewToken()
	if err := s.Store.CreateAuthStep(r.Context(), store.HashToken(token),
		store.AuthStep{Purpose: purpose, UserID: userID, Data: string(data), DesktopFlow: flow}, s.Now().Add(stepTTL)); err != nil {
		return err
	}
	s.setCookie(w, "fling_passkey", token, stepTTL)
	return nil
}

func (s *Server) takeCeremony(w http.ResponseWriter, r *http.Request, purpose string) (store.AuthStep, passkeyCeremony, bool) {
	var c passkeyCeremony
	cookie, err := r.Cookie(s.cookieName("fling_passkey"))
	s.clearCookie(w, "fling_passkey")
	if err != nil {
		return store.AuthStep{}, c, false
	}
	st, err := s.Store.TakeAuthStep(r.Context(), store.HashToken(cookie.Value), purpose, s.Now())
	if err != nil || json.Unmarshal([]byte(st.Data), &c) != nil {
		return st, c, false
	}
	return st, c, true
}

// ---- adding a passkey ------------------------------------------------------------

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyRequest(w, r) {
		return
	}
	var req struct {
		Link string `json:"link"`
	}
	json.NewDecoder(io.LimitReader(r.Body, maxJSONBody)).Decode(&req)

	var acct store.Account
	if req.Link != "" {
		r.SetPathValue("token", req.Link)
		a, _, ok := s.linkAccount(r)
		if !ok {
			jsonError(w, http.StatusGone, "This link has expired. Ask your administrator for a new one.")
			return
		}
		acct = a
	} else {
		u, _, ok := s.browserUser(r)
		if !ok {
			jsonError(w, http.StatusUnauthorized, "Sign in first.")
			return
		}
		a, err := s.Store.AccountByID(r.Context(), u.ID)
		if err != nil {
			s.internalError(w, "load account", err)
			return
		}
		acct = a
	}
	handle, err := s.Store.EnsureWebAuthnID(r.Context(), acct.ID)
	if err != nil {
		s.internalError(w, "passkey handle", err)
		return
	}
	acct.WebAuthnID = handle
	user, err := s.loadPasskeyUser(r, acct)
	if err != nil {
		s.internalError(w, "load passkeys", err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(user.creds))
	for _, c := range user.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := s.WebAuthn.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		s.internalError(w, "begin passkey registration", err)
		return
	}
	if err := s.saveCeremony(w, r, "passkey_register", acct.ID, "", passkeyCeremony{Session: *session, Link: req.Link}); err != nil {
		s.internalError(w, "save passkey ceremony", err)
		return
	}
	writeJSON(w, http.StatusOK, creation)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyRequest(w, r) {
		return
	}
	st, c, ok := s.takeCeremony(w, r, "passkey_register")
	if !ok {
		jsonError(w, http.StatusGone, "That took too long. Try again.")
		return
	}
	acct, err := s.Store.AccountByID(r.Context(), st.UserID)
	if err != nil || acct.Disabled {
		jsonError(w, http.StatusForbidden, "This account can't add passkeys.")
		return
	}
	user, err := s.loadPasskeyUser(r, acct)
	if err != nil {
		s.internalError(w, "load passkeys", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	cred, err := s.WebAuthn.FinishRegistration(user, c.Session, r)
	if err != nil {
		s.Log.Info("passkey registration rejected", "err", err)
		jsonError(w, http.StatusBadRequest, "The passkey couldn't be added. Try again.")
		return
	}

	signIn := false
	if c.Link != "" {
		// From an invite or reset link: the link is used up and the person is signed in.
		if err := s.Store.UseAccountLink(r.Context(), store.HashToken(c.Link), s.Now()); err != nil {
			jsonError(w, http.StatusGone, "This link has expired. Ask your administrator for a new one.")
			return
		}
		if err := s.Store.DeleteSessions(r.Context(), acct.ID, ""); err != nil {
			s.internalError(w, "end old sessions", err)
			return
		}
		signIn = true
	}
	data, _ := json.Marshal(cred)
	now := s.Now()
	if err := s.Store.AddPasskey(r.Context(), store.Passkey{
		ID: base64.RawURLEncoding.EncodeToString(cred.ID), UserID: acct.ID, Name: "Passkey added " + now.UTC().Format("2006-01-02"),
		Credential: string(data), CreatedAt: now,
	}); err != nil {
		s.internalError(w, "save passkey", err)
		return
	}
	s.audit(r.Context(), acct.User, acct.OrgID, "passkey.added", acct.ID, "")
	if signIn {
		if err := s.startBrowserSession(w, r, acct.User, "passkey"); err != nil {
			s.internalError(w, "start session", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": "/account?notice=passkey"})
}

// ---- signing in with a passkey ------------------------------------------------------

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyRequest(w, r) {
		return
	}
	var req struct {
		Flow string `json:"flow"`
	}
	json.NewDecoder(io.LimitReader(r.Body, maxJSONBody)).Decode(&req)
	if !s.byIP.Allowed("ip:"+s.clientIP(r), s.Now()) {
		jsonError(w, http.StatusTooManyRequests, errTooManyAttempts.Error())
		return
	}
	assertion, session, err := s.WebAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.internalError(w, "begin passkey login", err)
		return
	}
	if err := s.saveCeremony(w, r, "passkey_login", "", req.Flow, passkeyCeremony{Session: *session}); err != nil {
		s.internalError(w, "save passkey ceremony", err)
		return
	}
	writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !s.passkeyRequest(w, r) {
		return
	}
	st, c, ok := s.takeCeremony(w, r, "passkey_login")
	if !ok {
		jsonError(w, http.StatusGone, "That took too long. Try again.")
		return
	}
	var acct store.Account
	find := func(rawID, userHandle []byte) (webauthn.User, error) {
		a, err := s.Store.AccountByWebAuthnID(r.Context(), userHandle)
		if err != nil {
			return nil, err
		}
		acct = a
		return s.loadPasskeyUser(r, a)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	_, cred, err := s.WebAuthn.FinishPasskeyLogin(find, c.Session, r)
	now := s.Now()
	if err != nil {
		s.byIP.Fail("ip:"+s.clientIP(r), now)
		s.Log.Info("passkey sign-in rejected", "err", err)
		jsonError(w, http.StatusUnauthorized, "That passkey isn't registered here. Sign in another way, then add it on your account page.")
		return
	}
	if cred.Authenticator.CloneWarning {
		// The signature counter went backwards: the authenticator may have been copied.
		s.audit(r.Context(), acct.User, acct.OrgID, "passkey.clone_warning", acct.ID, "")
		jsonError(w, http.StatusUnauthorized, "This passkey can't be trusted any more. Remove it on your account page and add it again.")
		return
	}
	if !s.allowed(acct.User) {
		jsonError(w, http.StatusForbidden, "This account can't sign in. Ask your administrator.")
		return
	}
	data, _ := json.Marshal(cred)
	if err := s.Store.UpdatePasskeyUse(r.Context(), base64.RawURLEncoding.EncodeToString(cred.ID), string(data), now); err != nil {
		s.Log.Warn("update passkey", "err", err)
	}
	if err := s.startBrowserSession(w, r, acct.User, "passkey"); err != nil {
		s.internalError(w, "start session", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"redirect": signedInTarget(st.DesktopFlow)})
}

// ---- the page script ----------------------------------------------------------------

//go:embed static/fling.js
var static embed.FS

func (s *Server) script(w http.ResponseWriter, r *http.Request) {
	body, _ := static.ReadFile("static/fling.js")
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	http.ServeContent(w, r, "fling.js", time.Time{}, bytes.NewReader(body))
}

// NewWebAuthn configures passkeys for the gateway's public address.
func NewWebAuthn(publicURL, rpID string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{RPID: rpID, RPDisplayName: "Fling", RPOrigins: []string{publicURL}})
}
