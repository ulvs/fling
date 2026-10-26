package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/store"
)

const (
	loginTTL          = 10 * time.Minute
	desktopFlowTTL    = 10 * time.Minute
	desktopCodeTTL    = 2 * time.Minute
	browserSessionTTL = 7 * 24 * time.Hour
	desktopSessionTTL = 90 * 24 * time.Hour
	// A desktop session's expiry moves forward when it's used, at most this often.
	touchInterval = time.Hour
)

// ---- cookies and sessions ---------------------------------------------------

func (s *Server) secure() bool { return strings.HasPrefix(s.PublicURL, "https://") }

// cookieName adds the __Host- prefix on https, which pins the cookie to this host and path /.
func (s *Server) cookieName(base string) string {
	if s.secure() {
		return "__Host-" + base
	}
	return base
}

func (s *Server) setCookie(w http.ResponseWriter, base, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(base), Value: value, Path: "/", MaxAge: int(maxAge / time.Second),
		HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, base string) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(base), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure(), SameSite: http.SameSiteLaxMode,
	})
}

// browserUser returns the user signed in with the session cookie, if any.
func (s *Server) browserUser(r *http.Request) (store.User, string, bool) {
	c, err := r.Cookie(s.cookieName("fling_session"))
	if err != nil || c.Value == "" {
		return store.User{}, "", false
	}
	hash := store.HashToken(c.Value)
	se, err := s.Store.SessionByHash(r.Context(), hash, s.Now())
	if err != nil || se.Kind != "browser" || !s.allowed(se.User) {
		return store.User{}, "", false
	}
	return se.User, hash, true
}

// sameOrigin protects form posts: browsers send Sec-Fetch-Site or Origin on every POST.
func (s *Server) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	return r.Header.Get("Origin") == s.PublicURL
}

// ---- pages --------------------------------------------------------------------

type providerButton struct{ ID, Name, URL string }

type pageData struct {
	Title     string
	Message   string
	Email     string
	Providers []providerButton
	Links     []linkResponse
	FormURL   string
	Now       time.Time

	// Local accounts
	Local    bool   // show the passkey button and the email/password form
	Mail     bool   // "Forgot your password?" works
	Flow     string // a desktop sign-in waiting for this one
	Problem  string // what went wrong, shown above the form
	Notice   string // a confirmation, e.g. "Password changed."
	QR       template.HTML
	Secret   string
	Codes    []string
	Account  *accountView
	LinkCode string // invite/reset token, for the passkey button on the link page
	Admin    bool   // show the link to the admin pages
}

func (s *Server) render(w http.ResponseWriter, status int, name string, d pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pages.ExecuteTemplate(w, name, d); err != nil {
		s.Log.Error("render page", "page", name, "err", err)
	}
}

func (s *Server) page(w http.ResponseWriter, status int, title, message string) {
	s.render(w, status, "message", pageData{Title: title, Message: message})
}

func (s *Server) providerButtons(desktopFlow string) []providerButton {
	out := make([]providerButton, 0, len(s.Providers))
	for _, p := range s.Providers {
		u := "/auth/login/" + p.ID
		if desktopFlow != "" {
			u += "?flow=" + url.QueryEscape(desktopFlow)
		}
		out = append(out, providerButton{ID: p.ID, Name: p.Name, URL: u})
	}
	return out
}

func (s *Server) signinPage(title, flow string) pageData {
	return pageData{Title: title, Providers: s.providerButtons(flow), Local: s.Local, Mail: s.Local && s.Mail != nil, Flow: flow}
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.browserUser(r)
	if !ok {
		s.render(w, http.StatusOK, "signin", s.signinPage("Sign in", ""))
		return
	}
	links, err := s.Store.ListLinks(r.Context(), user.OrgID, user.ID)
	if err != nil {
		s.Log.Error("list links", "err", err)
		s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
		return
	}
	d := pageData{Title: "Your links", Email: user.Email, Now: s.Now(), Admin: s.isAdmin(user)}
	for _, l := range links {
		d.Links = append(d.Links, s.toLinkResponse(l))
	}
	s.render(w, http.StatusOK, "home", d)
}

// ---- browser sign-in ----------------------------------------------------------

func (s *Server) provider(id string) *auth.Provider {
	for _, p := range s.Providers {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	p := s.provider(r.PathValue("provider"))
	if p == nil {
		s.page(w, http.StatusNotFound, "Page not found", "This sign-in option isn't set up on this gateway.")
		return
	}
	now := s.Now()
	if err := s.Store.DeleteExpired(r.Context(), now); err != nil {
		s.Log.Warn("delete expired", "err", err)
	}
	ls := store.LoginState{
		State: store.NewToken(), Provider: p.ID, Nonce: store.NewToken(), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: "/", DesktopFlow: r.URL.Query().Get("flow"), ExpiresAt: now.Add(loginTTL),
	}
	if ls.DesktopFlow != "" {
		if _, err := s.Store.PendingDesktopFlow(r.Context(), ls.DesktopFlow, now); err != nil {
			s.page(w, http.StatusGone, "This sign-in has expired", "Start again from Fling on your computer.")
			return
		}
	}
	target, err := p.AuthURL(ls.State, ls.Nonce, ls.Verifier)
	if err != nil {
		s.Log.Error("sign-in provider unavailable", "provider", p.ID, "err", err)
		s.page(w, http.StatusBadGateway, p.Name+" sign-in isn't reachable", "Try again in a moment.")
		return
	}
	if err := s.Store.CreateLoginState(r.Context(), ls); err != nil {
		s.Log.Error("create login state", "err", err)
		s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
		return
	}
	// Binds the sign-in to this browser, so a link to someone else's callback can't sign you in.
	s.setCookie(w, "fling_login", ls.State, loginTTL)
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := s.provider(r.PathValue("provider"))
	if p == nil {
		s.page(w, http.StatusNotFound, "Page not found", "This sign-in option isn't set up on this gateway.")
		return
	}
	if q.Get("error") != "" {
		s.page(w, http.StatusUnauthorized, "Sign-in was cancelled", "Go back and try again.")
		return
	}
	c, err := r.Cookie(s.cookieName("fling_login"))
	s.clearCookie(w, "fling_login")
	if err != nil || q.Get("state") == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(q.Get("state"))) != 1 {
		s.page(w, http.StatusBadRequest, "Sign-in didn't finish", "Start again from the sign-in page, in the same browser.")
		return
	}
	now := s.Now()
	ls, err := s.Store.TakeLoginState(r.Context(), q.Get("state"), now)
	if err != nil || ls.Provider != p.ID {
		s.page(w, http.StatusBadRequest, "Sign-in expired", "Start again from the sign-in page.")
		return
	}
	id, err := p.Exchange(r.Context(), q.Get("code"), ls.Verifier, ls.Nonce)
	if errors.Is(err, auth.ErrNoVerifiedEmail) {
		s.page(w, http.StatusForbidden, "No verified email address", "Your "+p.Name+" account didn't share a verified email address, so Fling can't check that you're a member.")
		return
	} else if err != nil {
		s.Log.Error("sign-in failed", "provider", p.ID, "err", err)
		s.page(w, http.StatusBadGateway, "Sign-in didn't work", "Try again in a moment.")
		return
	}
	existing, err := s.Store.AccountByEmail(r.Context(), s.OrgID, id.Email)
	if err == nil && existing.Disabled {
		s.page(w, http.StatusForbidden, "This account is turned off", "Ask your administrator if you think this is a mistake.")
		return
	}
	invited := err == nil && existing.Local && s.Local
	if !invited && !s.memberEmail(id.Email) && !s.adminEmail(id.Email) {
		s.Log.Info("sign-in refused: not a member", "provider", p.ID)
		s.audit(r.Context(), store.User{}, s.OrgID, "signin.refused", "", "provider="+p.ID)
		s.page(w, http.StatusForbidden, "You're not a member yet", id.Email+" isn't on this gateway's member list. Ask your administrator to add you.")
		return
	}
	user, err := s.Store.EnsureUser(r.Context(), s.OrgID, id.Email, now)
	if err == nil {
		err = s.startBrowserSession(w, r, user, p.ID)
	}
	if err != nil {
		s.Log.Error("create session", "err", err)
		s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
		return
	}
	target := ls.ReturnTo
	if ls.DesktopFlow != "" {
		target = signedInTarget(ls.DesktopFlow)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// allowed decides who may use the gateway: accounts that aren't turned off, and are either
// invited local accounts, on the member list, or admins by configuration.
func (s *Server) allowed(u store.User) bool {
	return !u.Disabled && ((u.Local && s.Local) || s.memberEmail(u.Email) || s.adminEmail(u.Email))
}

// isAdmin: an allowed account with the admin flag, or named in FLING_ADMIN_EMAILS.
func (s *Server) isAdmin(u store.User) bool {
	return s.allowed(u) && (u.Admin || s.adminEmail(u.Email))
}

func (s *Server) memberEmail(email string) bool { return s.Members != nil && s.Members.Allows(email) }

func (s *Server) adminEmail(email string) bool { return s.Admins != nil && s.Admins.Allows(email) }

// startBrowserSession signs user in on this browser. method is recorded in the audit log.
func (s *Server) startBrowserSession(w http.ResponseWriter, r *http.Request, user store.User, method string) error {
	now := s.Now()
	if err := s.Store.RecordLogin(r.Context(), user.ID, now); err != nil {
		return err
	}
	token := store.NewToken()
	if err := s.Store.CreateSession(r.Context(), store.HashToken(token), user, "browser", now.Add(browserSessionTTL), now); err != nil {
		return err
	}
	s.audit(r.Context(), user, user.OrgID, "signin", user.ID, "method="+method)
	s.setCookie(w, "fling_session", token, browserSessionTTL)
	return nil
}

// signedInTarget is where a browser goes after signing in: back to a desktop sign-in that's
// waiting for it, or the start page.
func signedInTarget(desktopFlow string) string {
	if desktopFlow != "" {
		return "/auth/desktop/" + url.PathEscape(desktopFlow)
	}
	return "/"
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Sign-out blocked", "Use the Sign out button on this site.")
		return
	}
	if _, hash, ok := s.browserUser(r); ok {
		if err := s.Store.DeleteSession(r.Context(), hash); err != nil {
			s.Log.Error("delete session", "err", err)
		}
	}
	s.clearCookie(w, "fling_session")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- desktop sign-in (RFC 8252: loopback redirect + PKCE) ----------------------

// loopbackRedirect accepts only http://127.0.0.1:port/..., http://[::1]:port/... or
// http://localhost:port/..., so the one-time code can only reach the user's own computer.
func loopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || u.Port() == "" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) desktopStart(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !loopbackRedirect(q.Get("redirect_uri")) || q.Get("state") == "" ||
		q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 {
		s.page(w, http.StatusBadRequest, "This sign-in link isn't valid", "Start again from Fling on your computer.")
		return
	}
	f := store.DesktopFlow{
		ID: store.NewToken(), RedirectURI: q.Get("redirect_uri"), ClientState: q.Get("state"),
		CodeChallenge: q.Get("code_challenge"), ExpiresAt: s.Now().Add(desktopFlowTTL),
	}
	if err := s.Store.CreateDesktopFlow(r.Context(), f); err != nil {
		s.Log.Error("create desktop flow", "err", err)
		s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
		return
	}
	http.Redirect(w, r, "/auth/desktop/"+url.PathEscape(f.ID), http.StatusSeeOther)
}

// desktopConfirm asks a signed-in user to approve the desktop sign-in, so a program on the
// computer can't silently borrow an existing browser session.
func (s *Server) desktopConfirm(w http.ResponseWriter, r *http.Request) {
	flowID := r.PathValue("flow")
	if _, err := s.Store.PendingDesktopFlow(r.Context(), flowID, s.Now()); err != nil {
		s.page(w, http.StatusGone, "This sign-in has expired", "Start again from Fling on your computer.")
		return
	}
	user, _, ok := s.browserUser(r)
	if !ok {
		s.render(w, http.StatusOK, "signin", s.signinPage("Sign in to Fling", flowID))
		return
	}
	s.render(w, http.StatusOK, "desktop", pageData{Title: "Sign in to Fling", Email: user.Email, FormURL: "/auth/desktop/" + url.PathEscape(flowID)})
}

func (s *Server) desktopApprove(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Sign-in blocked", "Use the Continue button on this site.")
		return
	}
	user, _, ok := s.browserUser(r)
	if !ok {
		http.Redirect(w, r, "/auth/desktop/"+url.PathEscape(r.PathValue("flow")), http.StatusSeeOther)
		return
	}
	now := s.Now()
	f, err := s.Store.PendingDesktopFlow(r.Context(), r.PathValue("flow"), now)
	if err != nil {
		s.page(w, http.StatusGone, "This sign-in has expired", "Start again from Fling on your computer.")
		return
	}
	code := store.NewToken()
	if err := s.Store.ApproveDesktopFlow(r.Context(), f.ID, user, store.HashToken(code), now.Add(desktopCodeTTL), now); err != nil {
		s.page(w, http.StatusGone, "This sign-in has expired", "Start again from Fling on your computer.")
		return
	}
	s.audit(r.Context(), user, user.OrgID, "desktop.approve", user.ID, "")
	back, _ := url.Parse(f.RedirectURI)
	q := back.Query()
	q.Set("code", code)
	q.Set("state", f.ClientState)
	back.RawQuery = q.Encode()
	http.Redirect(w, r, back.String(), http.StatusSeeOther)
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"`
	ExpiresAt   time.Time `json:"expires_at"`
	Email       string    `json:"email"`
}

func oauthError(w http.ResponseWriter, code, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": description})
}

// token exchanges a desktop one-time code (form-encoded, as in OAuth) for a bearer token.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
		oauthError(w, "unsupported_grant_type", "Use grant_type=authorization_code.")
		return
	}
	now := s.Now()
	f, err := s.Store.TakeDesktopCode(r.Context(), store.HashToken(r.PostForm.Get("code")), now)
	if err != nil {
		oauthError(w, "invalid_grant", "The code is unknown, used or expired.")
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if r.PostForm.Get("redirect_uri") != f.RedirectURI || subtle.ConstantTimeCompare([]byte(challenge), []byte(f.CodeChallenge)) != 1 {
		oauthError(w, "invalid_grant", "The code verifier or redirect URI doesn't match.")
		return
	}
	user, err := s.Store.UserByID(r.Context(), f.UserID)
	if err != nil || !s.allowed(user) {
		oauthError(w, "invalid_grant", "This account can't sign in.")
		return
	}
	token := store.NewToken()
	expires := now.Add(desktopSessionTTL)
	if err := s.Store.CreateSession(r.Context(), store.HashToken(token), user, "desktop", expires, now); err != nil {
		s.internalError(w, "create desktop session", err)
		return
	}
	s.audit(r.Context(), user, user.OrgID, "desktop.signin", user.ID, "")
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(desktopSessionTTL / time.Second),
		ExpiresAt: expires.UTC(), Email: user.Email,
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	writeJSON(w, http.StatusOK, map[string]string{"email": user.Email})
}

// signOut ends the desktop session whose token made the call.
func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if hash, ok := r.Context().Value(tokenKey{}).(string); ok {
		if err := s.Store.DeleteSession(r.Context(), hash); err != nil {
			s.internalError(w, "delete session", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- templates ----------------------------------------------------------------

var pages = template.Must(template.New("pages").Funcs(template.FuncMap{
	"date": func(t time.Time) string { return t.UTC().Format("2006-01-02") },
	"downloads": func(n int64) string {
		if n == 1 {
			return "1 download"
		}
		return strconv.FormatInt(n, 10) + " downloads"
	},
	"active": func(l linkResponse, now time.Time) bool {
		return l.RevokedAt == nil && now.Before(l.ExpiresAt)
	},
}).Parse(`
{{define "head"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Fling</title>
<style>
:root{color-scheme:light dark;--surface:#FAF8F5;--raised:#FFFFFF;--ink:#1E1C1A;--muted:#6B655E;--line:#E6E1DA;--strong:#8F877D;--fling:#BD3514;--on-fling:#FFFFFF;--done:#2E7D50}
@media (prefers-color-scheme:dark){:root{--surface:#171615;--raised:#211F1D;--ink:#F2EEE8;--muted:#A8A198;--line:#34312D;--strong:#7D766D;--fling:#FF7A52;--on-fling:#1E1C1A;--done:#5CC48A}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;background:var(--surface);color:var(--ink);font:14px/20px "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif}
.center{min-height:100vh;display:grid;place-items:center;padding:16px}
.card{width:100%;max-width:420px;padding:24px;background:var(--raised);border:1px solid var(--line);border-radius:10px}
.wide{max-width:720px;margin:32px auto;padding:0 16px}
h1{font-size:20px;line-height:28px;margin:0 0 4px;font-weight:600}
p{margin:0 0 16px;color:var(--muted)}
.logo{display:flex;align-items:center;gap:8px;margin-bottom:24px;font-weight:600;font-size:16px}
.stack{display:grid;gap:8px}
.btn{display:block;width:100%;padding:8px 16px;border-radius:6px;border:1px solid var(--strong);background:var(--raised);color:var(--ink);font:inherit;font-weight:600;text-align:center;text-decoration:none;cursor:pointer}
.btn.primary{background:var(--fling);border-color:var(--fling);color:var(--on-fling)}
.btn.quiet{border-color:transparent;background:transparent;width:auto;display:inline-block;padding:4px 8px}
.btn:focus-visible{outline:2px solid var(--ink);outline-offset:2px}
.top{display:flex;justify-content:space-between;align-items:center;gap:16px;margin-bottom:24px}
.who{color:var(--muted)}
ul.links{list-style:none;margin:0;padding:0;border:1px solid var(--line);border-radius:10px;background:var(--raised)}
ul.links li{padding:12px 16px;border-top:1px solid var(--line);display:flex;justify-content:space-between;gap:16px}
ul.links li:first-child{border-top:0}
.name{overflow-wrap:anywhere}
.meta{color:var(--muted);font:12px/16px "Cascadia Mono",Consolas,monospace;white-space:nowrap}
.ok{color:var(--done)}
label{display:grid;gap:4px;font-weight:600}
input{font:inherit;font-weight:400;padding:7px 10px;border:1px solid var(--strong);border-radius:6px;background:var(--surface);color:var(--ink)}
input:focus-visible{outline:2px solid var(--ink);outline-offset:1px}
.or{display:flex;align-items:center;gap:8px;color:var(--muted);margin:16px 0}
.or::before,.or::after{content:"";flex:1;border-top:1px solid var(--line)}
.problem{color:var(--ink);background:var(--surface);border:1px solid var(--strong);border-radius:6px;padding:8px 12px}
.problem::before{content:"\26A0\FE0E  "}
.notice{border:1px solid var(--line);border-radius:6px;padding:8px 12px}
.notice::before{content:"\2713  ";color:var(--done)}
.small{font-size:12px;line-height:16px;color:var(--muted)}
.codes{font:14px/24px "Cascadia Mono",Consolas,monospace;columns:2;padding:12px 16px;border:1px solid var(--line);border-radius:6px;margin:0 0 16px}
.qr{display:block;width:200px;height:200px;margin:0 auto 16px;background:#fff;padding:8px;border-radius:6px}
section{margin:0 0 24px}
h2{font-size:16px;line-height:24px;margin:0 0 8px}
[hidden]{display:none!important}
.admin{max-width:1040px}
.tabs{display:flex;gap:4px;border-bottom:1px solid var(--line);margin:0 0 24px}
.tabs a{padding:8px 12px;color:var(--muted);text-decoration:none;border-bottom:2px solid transparent;margin-bottom:-1px}
.tabs a[aria-current]{color:var(--ink);border-bottom-color:var(--fling);font-weight:600}
.tabs a:focus-visible{outline:2px solid var(--ink)}
table{width:100%;border-collapse:collapse;background:var(--raised);border:1px solid var(--line);border-radius:10px;overflow:hidden;margin:0 0 16px}
th{text-align:left;font-weight:600;padding:8px 12px;border-bottom:1px solid var(--line);color:var(--muted)}
td{padding:10px 12px;border-top:1px solid var(--line);vertical-align:top}
tr.off td{opacity:.6}
td.actions{text-align:right;white-space:nowrap}
td.actions form{display:inline}
.tag{font-size:12px;padding:0 6px;border:1px solid var(--line);border-radius:999px;color:var(--muted)}
.sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0)}
.inline-form{display:flex;flex-wrap:wrap;gap:12px;align-items:end;margin:0 0 8px}
.inline-form label{min-width:260px}
.inline-form .btn{width:auto}
.rules{max-width:480px}
.rules .btn{width:auto}
.rules .small{margin:-4px 0 4px}
.sub{color:var(--muted);font-size:12px;line-height:16px}
label.check{display:flex;flex-direction:row;align-items:center;gap:6px;min-width:0;font-weight:400}
.linkbox{margin:0 0 16px}
.linkbox input{width:100%;font:13px/20px "Cascadia Mono",Consolas,monospace}
</style></head><body>{{end}}

{{define "logo"}}<div class="logo"><svg width="24" height="24" viewBox="0 0 512 512" aria-hidden="true"><rect width="512" height="512" rx="119.47" fill="#BD3514"/><circle cx="93.87" cy="426.67" r="21.33" fill="#FDE7DF"/><path d="M93.87 426.67 Q145.07 170.67 324.27 153.6" fill="none" stroke="#FDE7DF" stroke-width="42.67" stroke-linecap="round"/><circle cx="392.53" cy="162.13" r="55.47" fill="#F2B73A"/></svg><span>Fling Gateway</span></div>{{end}}

{{define "message"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}<h1>{{.Title}}</h1><p>{{.Message}}</p><a class="btn" href="/">Go to the start page</a></div></main></body></html>{{end}}

{{define "signin"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Only members of this gateway can sign in.</p>
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
{{if .Local}}<div class="stack">
<button class="btn primary" type="button" data-passkey-login data-flow="{{.Flow}}">Sign in with a passkey</button>
<p class="problem" data-passkey-error role="alert" hidden></p></div>
<div class="or">or</div>
<form method="post" action="/auth/password" class="stack">
<input type="hidden" name="flow" value="{{.Flow}}">
<label>Email <input name="email" type="email" autocomplete="username webauthn" required value="{{.Email}}"></label>
<label>Password <input name="password" type="password" autocomplete="current-password" required></label>
<button class="btn" type="submit">Sign in</button>
</form>
{{if .Mail}}<p class="small" style="margin:8px 0 0"><a href="/auth/forgot">Forgot your password?</a></p>{{end}}
{{if .Providers}}<div class="or">or</div>{{end}}{{end}}
<div class="stack">{{range .Providers}}<a class="btn" href="{{.URL}}">Continue with {{.Name}}</a>{{else}}{{if not .Local}}<p>No sign-in options are set up yet.</p>{{end}}{{end}}</div>
</div></main>{{if .Local}}<script src="/static/fling.js" defer></script>{{end}}</body></html>{{end}}

{{define "desktop"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Fling on your computer will share files as <strong>{{.Email}}</strong>.</p>
<form method="post" action="{{.FormURL}}" class="stack"><button class="btn primary" type="submit">Continue</button></form>
<p style="margin:16px 0 0">Not you? <a href="/">Sign out on the start page</a> first.</p>
</div></main></body></html>{{end}}

{{define "home"}}{{template "head" .}}<main class="wide">
<div class="top">{{template "logo"}}<form method="post" action="/auth/logout">{{if .Admin}}<a class="btn quiet" href="/admin">Admin</a> {{end}}<a class="who" href="/account">{{.Email}}</a> <button class="btn quiet" type="submit">Sign out</button></form></div>
<h1>Your links</h1><p>Share files with "Fling it" in File Explorer. Links you create appear here.</p>
{{if .Links}}<ul class="links">{{range .Links}}<li><span class="name">{{.Name}}</span><span class="meta">{{if active . $.Now}}<span class="ok">Active</span> · expires {{date .ExpiresAt}}{{else if .RevokedAt}}Turned off{{else}}Expired{{end}} · {{downloads .Downloads}}</span></li>{{end}}</ul>
{{else}}<p>No links yet.</p>{{end}}
</main></body></html>{{end}}
`))
