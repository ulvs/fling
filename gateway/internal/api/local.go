package api

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/mail"
	"github.com/ulvs/fling/gateway/internal/store"
)

const (
	stepTTL         = 10 * time.Minute
	maxCodeAttempts = 5
	InviteTTL       = 7 * 24 * time.Hour
	ResetTTL        = 24 * time.Hour
	forgotTTL       = time.Hour
)

var errTooManyAttempts = errors.New("Too many attempts. Wait 15 minutes and try again.")

// ---- password sign-in -------------------------------------------------------------

func (s *Server) passwordSignIn(w http.ResponseWriter, r *http.Request) {
	if !s.Local {
		s.page(w, http.StatusNotFound, "Page not found", "Local accounts aren't turned on.")
		return
	}
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Sign-in blocked", "Use the sign-in form on this site.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	flow := r.PostFormValue("flow")
	retry := func(status int, problem string) {
		d := s.signinPage("Sign in", flow)
		d.Email, d.Problem = email, problem
		s.render(w, status, "signin", d)
	}
	now := s.Now()
	emailKey, ipKey := "email:"+email, "ip:"+s.clientIP(r)
	if !s.byEmail.Allowed(emailKey, now) || !s.byIP.Allowed(ipKey, now) {
		retry(http.StatusTooManyRequests, errTooManyAttempts.Error())
		return
	}
	acct, err := s.Store.AccountByEmail(r.Context(), s.OrgID, email)
	hash := ""
	if err == nil && acct.Local && s.allowed(acct.User) {
		hash = acct.PasswordHash
	}
	if !auth.CheckPasswordOrDummy(r.PostFormValue("password"), hash) {
		s.byEmail.Fail(emailKey, now)
		s.byIP.Fail(ipKey, now)
		s.audit(r.Context(), store.User{}, s.OrgID, "signin.failed", acct.ID, "method=password")
		retry(http.StatusUnauthorized, "That email address and password don't match.")
		return
	}
	s.byEmail.Reset(emailKey)

	switch {
	case acct.TOTPSecret != "":
		s.startStep(w, r, "totp", acct.ID, flow)
		http.Redirect(w, r, "/auth/code", http.StatusSeeOther)
	case s.RequireTwoFactor:
		s.startStep(w, r, "totp_setup", acct.ID, flow)
		http.Redirect(w, r, "/auth/code/setup", http.StatusSeeOther)
	default:
		if err := s.startBrowserSession(w, r, acct.User, "password"); err != nil {
			s.internalPage(w, "start session", err)
			return
		}
		http.Redirect(w, r, s.signedInTarget(w, r, flow), http.StatusSeeOther)
	}
}

// startStep remembers a half-done sign-in in the database and binds it to this browser.
func (s *Server) startStep(w http.ResponseWriter, r *http.Request, purpose, userID, flow string) {
	token := store.NewToken()
	if err := s.Store.CreateAuthStep(r.Context(), store.HashToken(token),
		store.AuthStep{Purpose: purpose, UserID: userID, DesktopFlow: flow}, s.Now().Add(stepTTL)); err != nil {
		s.Log.Error("create auth step", "err", err)
	}
	s.setCookie(w, "fling_step", token, stepTTL)
}

// step returns the half-done sign-in of this browser with the given purpose.
func (s *Server) step(r *http.Request, purpose string) (store.AuthStep, string, bool) {
	c, err := r.Cookie(s.cookieName("fling_step"))
	if err != nil || c.Value == "" {
		return store.AuthStep{}, "", false
	}
	hash := store.HashToken(c.Value)
	st, err := s.Store.AuthStepByHash(r.Context(), hash, purpose, s.Now())
	return st, hash, err == nil
}

func (s *Server) internalPage(w http.ResponseWriter, what string, err error) {
	s.Log.Error(what, "err", err)
	s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
}

// ---- two-step codes at sign-in ---------------------------------------------------------

func (s *Server) codePage(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.step(r, "totp"); !ok {
		s.page(w, http.StatusGone, "Sign-in expired", "Start again from the sign-in page.")
		return
	}
	s.render(w, http.StatusOK, "code", pageData{Title: "Enter your code"})
}

func (s *Server) codeSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Sign-in blocked", "Use the form on this site.")
		return
	}
	st, hash, ok := s.step(r, "totp")
	if !ok {
		s.page(w, http.StatusGone, "Sign-in expired", "Start again from the sign-in page.")
		return
	}
	acct, err := s.Store.AccountByID(r.Context(), st.UserID)
	if err != nil || !s.allowed(acct.User) {
		s.page(w, http.StatusForbidden, "This account can't sign in", "Ask your administrator.")
		return
	}
	now := s.Now()
	key := "code:" + acct.ID
	if !s.byEmail.Allowed(key, now) {
		s.render(w, http.StatusTooManyRequests, "code", pageData{Title: "Enter your code", Problem: errTooManyAttempts.Error()})
		return
	}
	method, ok := s.checkSecondFactor(r.Context(), acct, r.PostFormValue("code"), now)
	if !ok {
		s.byEmail.Fail(key, now)
		if err := s.Store.FailAuthStep(r.Context(), hash, maxCodeAttempts); err != nil {
			s.Log.Error("fail auth step", "err", err)
		}
		s.render(w, http.StatusUnauthorized, "code", pageData{Title: "Enter your code", Problem: "That code didn't work. Codes change every 30 seconds."})
		return
	}
	s.byEmail.Reset(key)
	if _, err := s.Store.TakeAuthStep(r.Context(), hash, "totp", now); err != nil {
		s.page(w, http.StatusGone, "Sign-in expired", "Start again from the sign-in page.")
		return
	}
	s.clearCookie(w, "fling_step")
	if err := s.startBrowserSession(w, r, acct.User, method); err != nil {
		s.internalPage(w, "start session", err)
		return
	}
	http.Redirect(w, r, s.signedInTarget(w, r, st.DesktopFlow), http.StatusSeeOther)
}

// checkSecondFactor accepts a current authenticator code (once) or an unused recovery code.
func (s *Server) checkSecondFactor(ctx context.Context, acct store.Account, code string, now time.Time) (string, bool) {
	if acct.TOTPSecret == "" {
		return "", false
	}
	if step, ok := auth.CheckTOTP(acct.TOTPSecret, code, now, acct.TOTPLastStep); ok {
		return "password+code", s.Store.UseTOTPStep(ctx, acct.ID, step) == nil
	}
	if strings.TrimSpace(code) != "" && s.Store.UseRecoveryCode(ctx, acct.ID, auth.HashRecoveryCode(code), now) == nil {
		s.audit(ctx, acct.User, acct.OrgID, "recovery_code.used", acct.ID, "")
		return "password+recovery", true
	}
	return "", false
}

// ---- setting up two-step codes --------------------------------------------------------

// setupActor is who's setting up codes: a sign-in that requires them first, or a signed-in user.
func (s *Server) setupActor(r *http.Request) (acct store.Account, stepHash, flow string, ok bool) {
	if st, hash, found := s.step(r, "totp_setup"); found {
		a, err := s.Store.AccountByID(r.Context(), st.UserID)
		return a, hash, st.DesktopFlow, err == nil && s.allowed(a.User)
	}
	if u, _, found := s.browserUser(r); found {
		a, err := s.Store.AccountByID(r.Context(), u.ID)
		return a, "", "", err == nil
	}
	return store.Account{}, "", "", false
}

func (s *Server) issuer() string {
	if u, err := url.Parse(s.PublicURL); err == nil {
		return "Fling (" + u.Hostname() + ")"
	}
	return "Fling"
}

func (s *Server) codeSetupPage(w http.ResponseWriter, r *http.Request) {
	acct, _, _, ok := s.setupActor(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderCodeSetup(w, r, acct, http.StatusOK, "")
}

func (s *Server) renderCodeSetup(w http.ResponseWriter, r *http.Request, acct store.Account, status int, problem string) {
	secret := acct.TOTPPending
	if secret == "" {
		secret = auth.NewTOTPSecret()
		if err := s.Store.SetTOTPPending(r.Context(), acct.ID, secret); err != nil {
			s.internalPage(w, "set pending code secret", err)
			return
		}
	}
	d := pageData{Title: "Set up two-step sign-in", Email: acct.Email, Secret: secret, Problem: problem,
		QR: qrSVG(auth.TOTPURI(s.issuer(), acct.Email, secret))}
	s.render(w, status, "code-setup", d)
}

func (s *Server) codeSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Blocked", "Use the form on this site.")
		return
	}
	acct, stepHash, flow, ok := s.setupActor(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	now := s.Now()
	step, valid := auth.CheckTOTP(acct.TOTPPending, r.PostFormValue("code"), now, 0)
	if acct.TOTPPending == "" || !valid {
		if stepHash != "" {
			s.Store.FailAuthStep(r.Context(), stepHash, maxCodeAttempts)
		}
		s.renderCodeSetup(w, r, acct, http.StatusUnauthorized, "That code didn't work. Check the time on your phone and try the newest code.")
		return
	}
	codes := auth.NewRecoveryCodes(10)
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = auth.HashRecoveryCode(c)
	}
	if err := s.Store.EnableTOTP(r.Context(), acct.ID, step, hashes); err != nil {
		s.internalPage(w, "enable two-step codes", err)
		return
	}
	s.audit(r.Context(), acct.User, acct.OrgID, "totp.enabled", acct.ID, "")
	next := "/account"
	if stepHash != "" {
		// The sign-in that required codes can finish now.
		s.Store.TakeAuthStep(r.Context(), stepHash, "totp_setup", now)
		s.clearCookie(w, "fling_step")
		if err := s.startBrowserSession(w, r, acct.User, "password+code"); err != nil {
			s.internalPage(w, "start session", err)
			return
		}
		next = s.signedInTarget(w, r, flow)
	}
	s.render(w, http.StatusOK, "recovery-codes", pageData{Title: "Save your recovery codes", Codes: codes, FormURL: next})
}

func qrSVG(text string) template.HTML {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	var b strings.Builder
	n := code.Size + 8 // 4-module quiet zone each side
	fmt.Fprintf(&b, `<svg class="qr" viewBox="0 0 %d %d" role="img" aria-label="QR code for your authenticator app" shape-rendering="crispEdges"><path fill="#000" d="`, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return template.HTML(b.String()) // built only from numbers above
}

// ---- invite and reset links ----------------------------------------------------------

func (s *Server) linkAccount(r *http.Request) (store.Account, store.AccountLink, bool) {
	l, err := s.Store.AccountLinkByHash(r.Context(), store.HashToken(r.PathValue("token")), s.Now())
	if err != nil {
		return store.Account{}, l, false
	}
	acct, err := s.Store.AccountByID(r.Context(), l.UserID)
	return acct, l, err == nil && acct.Local && !acct.Disabled
}

func (s *Server) linkPage(w http.ResponseWriter, r *http.Request) {
	acct, l, ok := s.linkAccount(r)
	if !s.Local || !ok {
		s.page(w, http.StatusGone, "This link has expired", "Ask your administrator for a new one.")
		return
	}
	s.renderLink(w, http.StatusOK, acct, l, r.PathValue("token"), "")
}

func (s *Server) renderLink(w http.ResponseWriter, status int, acct store.Account, l store.AccountLink, token, problem string) {
	title := "Choose how you sign in"
	if l.Purpose == "invite" {
		title = "Welcome to Fling"
	}
	s.render(w, status, "link", pageData{Title: title, Email: acct.Email, LinkCode: token, Problem: problem,
		FormURL: "/auth/link/" + url.PathEscape(token)})
}

func (s *Server) linkSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Blocked", "Use the form on this site.")
		return
	}
	acct, l, ok := s.linkAccount(r)
	if !s.Local || !ok {
		s.page(w, http.StatusGone, "This link has expired", "Ask your administrator for a new one.")
		return
	}
	password := r.PostFormValue("password")
	if err := auth.CheckPasswordRules(password, acct.Email); err != nil {
		s.renderLink(w, http.StatusUnprocessableEntity, acct, l, r.PathValue("token"), err.Error())
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.internalPage(w, "hash password", err)
		return
	}
	if err := s.Store.UseAccountLink(r.Context(), store.HashToken(r.PathValue("token")), s.Now()); err != nil {
		s.page(w, http.StatusGone, "This link has expired", "Ask your administrator for a new one.")
		return
	}
	if err := s.Store.SetPasswordHash(r.Context(), acct.ID, hash); err == nil {
		err = s.Store.DeleteSessions(r.Context(), acct.ID, "")
	}
	if err != nil {
		s.internalPage(w, "set password", err)
		return
	}
	s.audit(r.Context(), acct.User, acct.OrgID, "password.set", acct.ID, "link="+l.Purpose)
	if s.RequireTwoFactor && acct.TOTPSecret == "" {
		s.startStep(w, r, "totp_setup", acct.ID, "")
		http.Redirect(w, r, "/auth/code/setup", http.StatusSeeOther)
		return
	}
	if err := s.startBrowserSession(w, r, acct.User, "password"); err != nil {
		s.internalPage(w, "start session", err)
		return
	}
	http.Redirect(w, r, "/account?notice=password", http.StatusSeeOther)
}

// CreateAccountLink makes an invite or reset link for a local account and emails it when
// mail is set up. It returns the link either way, for the admin to pass on.
func (s *Server) CreateAccountLink(ctx context.Context, acct store.User, purpose string, ttl time.Duration) (string, bool, error) {
	token := store.NewToken()
	now := s.Now()
	if err := s.Store.CreateAccountLink(ctx, store.HashToken(token), acct.ID, purpose, now.Add(ttl), now); err != nil {
		return "", false, err
	}
	link := s.PublicURL + "/auth/link/" + token
	if s.Mail == nil {
		return link, false, nil
	}
	err := s.Mail.Send(ctx, accountLinkMail(acct.Email, purpose, link, s.PublicURL, ttl))
	return link, err == nil, err
}

func accountLinkMail(to, purpose, link, site string, ttl time.Duration) mail.Message {
	valid := fmt.Sprintf("%d days", int(ttl.Hours()/24))
	if ttl < 48*time.Hour {
		valid = fmt.Sprintf("%d hours", int(ttl.Hours()))
	}
	subject, intro := "Reset how you sign in to Fling", "Use this link to choose a new password or add a passkey for "+site+"."
	if purpose == "invite" {
		subject, intro = "You're invited to Fling", "You've been invited to share files with Fling at "+site+". Use this link to set up how you sign in."
	}
	text := intro + "\n\n" + link + "\n\nThe link works once and expires in " + valid + ". If you didn't expect this email, you can ignore it.\n"
	html := "<p>" + template.HTMLEscapeString(intro) + `</p><p><a href="` + template.HTMLEscapeString(link) + `">` +
		template.HTMLEscapeString(link) + "</a></p><p>The link works once and expires in " + valid +
		". If you didn't expect this email, you can ignore it.</p>"
	return mail.Message{To: to, Subject: subject, Text: text, HTML: html}
}

// ---- forgotten passwords (only with mail) -----------------------------------------------

func (s *Server) forgotPage(w http.ResponseWriter, r *http.Request) {
	if !s.Local || s.Mail == nil {
		s.page(w, http.StatusNotFound, "Page not found", "Ask your administrator for a reset link.")
		return
	}
	s.render(w, http.StatusOK, "forgot", pageData{Title: "Forgot your password?"})
}

func (s *Server) forgotSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.Local || s.Mail == nil || !s.sameOrigin(r) {
		s.page(w, http.StatusNotFound, "Page not found", "Ask your administrator for a reset link.")
		return
	}
	now := s.Now()
	ipKey := "forgot:" + s.clientIP(r)
	if !s.byIP.Allowed(ipKey, now) {
		s.render(w, http.StatusTooManyRequests, "forgot", pageData{Title: "Forgot your password?", Problem: errTooManyAttempts.Error()})
		return
	}
	s.byIP.Fail(ipKey, now) // every request counts, so this can't be used to flood someone's inbox
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	if acct, err := s.Store.AccountByEmail(r.Context(), s.OrgID, email); err == nil && acct.Local && s.allowed(acct.User) {
		// Sent in the background, so the response takes as long whether or not the account exists.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, _, err := s.CreateAccountLink(ctx, acct.User, "reset", forgotTTL); err != nil {
				s.Log.Error("send reset link", "err", err)
			}
		}()
	}
	s.page(w, http.StatusOK, "Check your email", "If there's an account for that address, a link to reset your password is on its way. It works for one hour.")
}

// ---- account page -------------------------------------------------------------------

type accountView struct {
	Local       bool
	HasPassword bool
	TOTP        bool
	CodesLeft   int
	Required    bool // two-step codes are required by the gateway
	Passkeys    []store.Passkey
	CanPasskey  bool
}

var notices = map[string]string{
	"password":        "Password saved.",
	"passkey":         "Passkey added. You can use it to sign in now.",
	"passkey-removed": "Passkey removed.",
	"code-off":        "Two-step sign-in is off.",
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.browserUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderAccount(w, r, user, http.StatusOK, "", notices[r.URL.Query().Get("notice")])
}

func (s *Server) renderAccount(w http.ResponseWriter, r *http.Request, user store.User, status int, problem, notice string) {
	acct, err := s.Store.AccountByID(r.Context(), user.ID)
	if err != nil {
		s.internalPage(w, "load account", err)
		return
	}
	keys, err := s.Store.Passkeys(r.Context(), user.ID)
	if err != nil {
		s.internalPage(w, "list passkeys", err)
		return
	}
	left, _ := s.Store.RecoveryCodesLeft(r.Context(), user.ID)
	view := &accountView{Local: acct.Local, HasPassword: acct.PasswordHash != "", TOTP: acct.TOTPSecret != "",
		CodesLeft: left, Required: s.RequireTwoFactor, Passkeys: keys, CanPasskey: s.WebAuthn != nil}
	s.render(w, status, "account", pageData{Title: "Your account", Email: acct.Email, Account: view, Problem: problem, Notice: notice, Local: s.WebAuthn != nil})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	user, sessionHash, ok := s.browserUser(r)
	if !ok || !s.sameOrigin(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	acct, err := s.Store.AccountByID(r.Context(), user.ID)
	if err != nil || !acct.Local {
		s.renderAccount(w, r, user, http.StatusForbidden, "Only local accounts have a password here.", "")
		return
	}
	now := s.Now()
	key := "email:" + acct.Email
	if !s.byEmail.Allowed(key, now) {
		s.renderAccount(w, r, user, http.StatusTooManyRequests, errTooManyAttempts.Error(), "")
		return
	}
	if acct.PasswordHash != "" && !auth.CheckPasswordOrDummy(r.PostFormValue("current"), acct.PasswordHash) {
		s.byEmail.Fail(key, now)
		s.renderAccount(w, r, user, http.StatusUnauthorized, "Your current password isn't right.", "")
		return
	}
	password := r.PostFormValue("password")
	if err := auth.CheckPasswordRules(password, acct.Email); err != nil {
		s.renderAccount(w, r, user, http.StatusUnprocessableEntity, err.Error(), "")
		return
	}
	hash, err := auth.HashPassword(password)
	if err == nil {
		err = s.Store.SetPasswordHash(r.Context(), acct.ID, hash)
	}
	if err == nil {
		// Signs out everywhere else, including desktop apps, in case the old password leaked.
		err = s.Store.DeleteSessions(r.Context(), acct.ID, sessionHash)
	}
	if err != nil {
		s.internalPage(w, "change password", err)
		return
	}
	s.audit(r.Context(), user, user.OrgID, "password.changed", user.ID, "")
	http.Redirect(w, r, "/account?notice=password", http.StatusSeeOther)
}

func (s *Server) turnOffCodes(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.browserUser(r)
	if !ok || !s.sameOrigin(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.RequireTwoFactor {
		s.renderAccount(w, r, user, http.StatusForbidden, "This gateway requires two-step sign-in.", "")
		return
	}
	acct, err := s.Store.AccountByID(r.Context(), user.ID)
	if err != nil {
		s.internalPage(w, "load account", err)
		return
	}
	if _, ok := s.checkSecondFactor(r.Context(), acct, r.PostFormValue("code"), s.Now()); !ok {
		s.renderAccount(w, r, user, http.StatusUnauthorized, "That code didn't work.", "")
		return
	}
	if err := s.Store.DisableTOTP(r.Context(), user.ID); err != nil {
		s.internalPage(w, "disable two-step codes", err)
		return
	}
	s.audit(r.Context(), user, user.OrgID, "totp.disabled", user.ID, "")
	http.Redirect(w, r, "/account?notice=code-off", http.StatusSeeOther)
}

func (s *Server) removePasskey(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.browserUser(r)
	if !ok || !s.sameOrigin(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	acct, err := s.Store.AccountByID(r.Context(), user.ID)
	if err != nil {
		s.internalPage(w, "load account", err)
		return
	}
	if acct.Local && acct.PasswordHash == "" && acct.Passkeys <= 1 && !s.memberEmail(acct.Email) {
		s.renderAccount(w, r, user, http.StatusConflict, "This is your only way to sign in. Set a password or add another passkey first.", "")
		return
	}
	if err := s.Store.DeletePasskey(r.Context(), user.ID, r.PathValue("id")); err != nil {
		s.renderAccount(w, r, user, http.StatusNotFound, "That passkey is already gone.", "")
		return
	}
	s.audit(r.Context(), user, user.OrgID, "passkey.removed", user.ID, "")
	http.Redirect(w, r, "/account?notice=passkey-removed", http.StatusSeeOther)
}

func init() {
	template.Must(pages.Parse(localPages))
}

const localPages = `
{{define "code"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Open your authenticator app and enter the 6-digit code for Fling. Lost your phone? Enter one of your recovery codes instead.</p>
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
<form method="post" action="/auth/code" class="stack">
<label>Code <input name="code" autocomplete="one-time-code" inputmode="text" required autofocus></label>
<button class="btn primary" type="submit">Continue</button>
</form></div></main></body></html>{{end}}

{{define "code-setup"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Scan this with an authenticator app (Microsoft Authenticator, Google Authenticator, 1Password, …), then enter the code it shows.</p>
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
{{.QR}}
<p class="small">Can't scan it? Enter this key in the app: <span class="meta">{{.Secret}}</span></p>
<form method="post" action="/auth/code/setup" class="stack">
<label>Code from the app <input name="code" autocomplete="one-time-code" inputmode="numeric" pattern="[0-9 ]*" required></label>
<button class="btn primary" type="submit">Turn on two-step sign-in</button>
</form></div></main></body></html>{{end}}

{{define "recovery-codes"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>If you lose your phone, each of these codes signs you in once. Store them in a password manager or print them. They won't be shown again.</p>
<ol class="codes">{{range .Codes}}<li>{{.}}</li>{{end}}</ol>
<a class="btn primary" href="{{.FormURL}}">I've saved them</a>
</div></main></body></html>{{end}}

{{define "link"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Signing in as <strong>{{.Email}}</strong>. A passkey is the easiest and safest choice: you sign in with your fingerprint, face or device PIN.</p>
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
<div class="stack">
<button class="btn primary" type="button" data-passkey-register data-link="{{.LinkCode}}">Create a passkey</button>
<p class="problem" data-passkey-error role="alert" hidden></p></div>
<div class="or">or</div>
<form method="post" action="{{.FormURL}}" class="stack">
<input type="email" name="username" value="{{.Email}}" autocomplete="username" hidden readonly>
<label>Choose a password <input name="password" type="password" autocomplete="new-password" minlength="12" required></label>
<p class="small" style="margin:0">At least 12 characters. A few random words work well.</p>
<button class="btn" type="submit">Use this password</button>
</form></div></main><script src="/static/fling.js" defer></script></body></html>{{end}}

{{define "forgot"}}{{template "head" .}}<main class="center"><div class="card">{{template "logo"}}
<h1>{{.Title}}</h1><p>Enter your email address and we'll send you a link to choose a new one.</p>
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
<form method="post" action="/auth/forgot" class="stack">
<label>Email <input name="email" type="email" autocomplete="username" required></label>
<button class="btn primary" type="submit">Send the link</button>
</form></div></main></body></html>{{end}}

{{define "account"}}{{template "head" .}}<main class="wide">
<div class="top">{{template "logo"}}<a class="btn quiet" href="/">Back to your links</a></div>
<h1>{{.Title}}</h1><p>{{.Email}}</p>
{{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
{{with .Account}}
{{if .CanPasskey}}<section><h2>Passkeys</h2>
{{if .Passkeys}}<ul class="links">{{range .Passkeys}}<li><span class="name">{{.Name}}</span><span class="meta">added {{date .CreatedAt}}{{if .LastUsedAt}} · last used {{date .LastUsedAt}}{{end}}
<form method="post" action="/account/passkeys/{{.ID}}/remove" style="display:inline"><button class="btn quiet" type="submit">Remove</button></form></span></li>{{end}}</ul>
{{else}}<p>No passkeys yet. With one, you sign in with your fingerprint, face or device PIN.</p>{{end}}
<div class="stack" style="margin-top:8px;max-width:320px"><button class="btn primary" type="button" data-passkey-register>Add a passkey</button>
<p class="problem" data-passkey-error role="alert" hidden></p></div></section>{{end}}
{{if .Local}}<section><h2>Password</h2>
<form method="post" action="/account/password" class="stack" style="max-width:320px">
{{if .HasPassword}}<label>Current password <input name="current" type="password" autocomplete="current-password" required></label>{{end}}
<label>New password <input name="password" type="password" autocomplete="new-password" minlength="12" required></label>
<button class="btn" type="submit">{{if .HasPassword}}Change password{{else}}Set a password{{end}}</button>
</form><p class="small">Changing it signs you out everywhere else, including Fling on your computers.</p></section>
<section><h2>Two-step sign-in</h2>
{{if .TOTP}}<p>On. Signing in with a password also needs a code from your authenticator app. {{.CodesLeft}} recovery codes left.</p>
{{if not .Required}}<form method="post" action="/account/code/off" class="stack" style="max-width:320px">
<label>Code, to turn it off <input name="code" autocomplete="one-time-code" required></label>
<button class="btn" type="submit">Turn off</button></form>{{end}}
{{else}}<p>Off. Turn it on to also ask for a code from your phone when you sign in with a password.</p>
<a class="btn" style="max-width:320px" href="/auth/code/setup">Set up two-step sign-in</a>{{end}}</section>{{end}}
{{end}}
</main>{{if .Local}}<script src="/static/fling.js" defer></script>{{end}}</body></html>{{end}}
`
