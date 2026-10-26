package api

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ulvs/fling/gateway/internal/store"
)

// The admin pages: people (invite, turn off, reset sign-in, admins), every link in the
// organisation, and the activity log. Admins have the flag in the database or are named in
// FLING_ADMIN_EMAILS. Every change is a same-origin form POST and is written to the audit log.

type personRow struct {
	store.Account
	AdminByConfig bool
	IsAdmin       bool
	IsSelf        bool
}

type adminData struct {
	Title      string
	Email      string // the signed-in admin
	Tab        string
	Problem    string
	Notice     string
	ShownLink  string // an invite or reset link to pass on by hand
	Local      bool
	Mail       bool
	People     []personRow
	Links      []store.OrgLink
	Search     string
	ShowAll    bool
	Events     []store.AuditRow
	OlderThan  int64
	Now        time.Time
	PublicURL  string
	AdminCount int
	Cleanup    string // "a day" when files are deleted that long after their link ends; empty when off
}

// adminUser returns the signed-in admin, or answers the request itself.
func (s *Server) adminUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	u, _, ok := s.browserUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return u, false
	}
	if !s.isAdmin(u) {
		s.page(w, http.StatusForbidden, "Admins only", "Ask an admin of this gateway if you need access.")
		return u, false
	}
	if r.Method == http.MethodPost && !s.sameOrigin(r) {
		s.page(w, http.StatusForbidden, "Blocked", "Use the buttons on this site.")
		return u, false
	}
	return u, true
}

func (s *Server) renderAdmin(w http.ResponseWriter, status int, name string, d adminData) {
	d.Now, d.PublicURL = s.Now(), s.PublicURL
	if s.DeleteFiles {
		d.Cleanup = humanDelay(s.DeleteAfter)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pages.ExecuteTemplate(w, name, d); err != nil {
		s.Log.Error("render admin page", "page", name, "err", err)
	}
}

// ---- people ---------------------------------------------------------------------

var adminNotices = map[string]string{
	"disabled":      "Turned off and signed out everywhere.",
	"enabled":       "Turned back on.",
	"signed-out":    "Signed out everywhere, including Fling on their computers.",
	"codes-removed": "Two-step codes removed. They can set them up again on their account page.",
	"admin-added":   "Now an admin.",
	"admin-removed": "No longer an admin.",
	"revoked":       "Link turned off.",
}

func (s *Server) adminPeople(w http.ResponseWriter, r *http.Request) {
	u, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	s.renderPeople(w, r, u, http.StatusOK, adminData{Notice: adminNotices[r.URL.Query().Get("done")]})
}

func (s *Server) renderPeople(w http.ResponseWriter, r *http.Request, admin store.User, status int, d adminData) {
	accounts, err := s.Store.ListAccounts(r.Context(), s.OrgID)
	if err != nil {
		s.internalPage(w, "list accounts", err)
		return
	}
	for _, a := range accounts {
		row := personRow{Account: a, AdminByConfig: s.adminEmail(a.Email), IsSelf: a.ID == admin.ID}
		row.IsAdmin = !a.Disabled && (a.Admin || row.AdminByConfig)
		if row.IsAdmin {
			d.AdminCount++
		}
		d.People = append(d.People, row)
	}
	d.Title, d.Tab, d.Email = "People", "people", admin.Email
	d.Local, d.Mail = s.Local, s.Mail != nil
	s.renderAdmin(w, status, "admin-people", d)
}

func (s *Server) adminInvite(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	if !s.Local {
		s.renderPeople(w, r, admin, http.StatusConflict, adminData{Problem: "Local accounts are off on this gateway (FLING_LOCAL_ACCOUNTS)."})
		return
	}
	if at := strings.LastIndexByte(email, '@'); at <= 0 || at == len(email)-1 || strings.ContainsAny(email, " <>,;") {
		s.renderPeople(w, r, admin, http.StatusUnprocessableEntity, adminData{Problem: "That isn't an email address."})
		return
	}
	user, err := s.Store.EnsureLocalUser(r.Context(), s.OrgID, email, s.Now())
	if err != nil {
		s.internalPage(w, "invite", err)
		return
	}
	if user.Disabled {
		s.renderPeople(w, r, admin, http.StatusConflict, adminData{Problem: email + " is turned off. Turn the account on first."})
		return
	}
	if r.PostFormValue("admin") == "on" {
		if err := s.Store.SetAdmin(r.Context(), user.ID, true); err != nil {
			s.internalPage(w, "make admin", err)
			return
		}
		s.audit(r.Context(), admin, admin.OrgID, "admin.granted", user.ID, "")
	}
	link, sent, err := s.CreateAccountLink(r.Context(), user, "invite", InviteTTL)
	if link == "" {
		s.internalPage(w, "create invite", err)
		return
	}
	s.audit(r.Context(), admin, admin.OrgID, "admin.invite", user.ID, "")
	d := adminData{}
	switch {
	case sent:
		d.Notice = "Invite sent to " + email + ". The link works once, for 7 days."
	case err != nil:
		s.Log.Error("send invite", "err", err)
		d.Problem = "The email couldn't be sent. Pass this link on yourself; it works once, for 7 days."
		d.ShownLink = link
	default:
		d.Notice = "Invite created. Email isn't set up, so pass this link on yourself; it works once, for 7 days."
		d.ShownLink = link
	}
	s.renderPeople(w, r, admin, http.StatusOK, d)
}

// adminPersonAction handles the buttons on a person's row.
func (s *Server) adminPersonAction(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	target, err := s.Store.AccountByID(r.Context(), r.PathValue("id"))
	if err != nil || target.OrgID != admin.OrgID {
		s.renderPeople(w, r, admin, http.StatusNotFound, adminData{Problem: "That account doesn't exist any more."})
		return
	}
	action := r.PathValue("action")
	self := target.ID == admin.ID
	refuse := func(msg string) { s.renderPeople(w, r, admin, http.StatusConflict, adminData{Problem: msg}) }
	now := s.Now()

	switch action {
	case "disable":
		if self {
			refuse("You can't turn off your own account.")
			return
		}
		err = s.Store.SetDisabled(r.Context(), target.ID, true, now)
	case "enable":
		err = s.Store.SetDisabled(r.Context(), target.ID, false, now)
	case "sign-out":
		err = s.Store.DeleteSessions(r.Context(), target.ID, "")
	case "remove-codes":
		err = s.Store.DisableTOTP(r.Context(), target.ID)
	case "make-admin":
		err = s.Store.SetAdmin(r.Context(), target.ID, true)
	case "remove-admin":
		if self {
			refuse("You can't remove your own admin rights. Ask another admin.")
			return
		}
		if s.adminEmail(target.Email) {
			refuse(target.Email + " is an admin through FLING_ADMIN_EMAILS. Remove it there.")
			return
		}
		err = s.Store.SetAdmin(r.Context(), target.ID, false)
	case "reset":
		if !s.Local || !target.Local {
			refuse("Only local accounts sign in with a passkey or password. Others sign in with their provider.")
			return
		}
		if target.Disabled {
			refuse("Turn the account on first.")
			return
		}
		link, sent, sendErr := s.CreateAccountLink(r.Context(), target.User, "reset", ResetTTL)
		if link == "" {
			s.internalPage(w, "create reset link", sendErr)
			return
		}
		s.audit(r.Context(), admin, admin.OrgID, "admin.reset", target.ID, "")
		d := adminData{}
		if sent {
			d.Notice = "A link to choose a new password or passkey was sent to " + target.Email + ". It works once, for 24 hours."
		} else {
			if sendErr != nil {
				s.Log.Error("send reset link", "err", sendErr)
			}
			d.Notice = "Pass this link to " + target.Email + " yourself. It works once, for 24 hours."
			d.ShownLink = link
		}
		s.renderPeople(w, r, admin, http.StatusOK, d)
		return
	default:
		s.page(w, http.StatusNotFound, "Page not found", "")
		return
	}
	if err != nil {
		s.internalPage(w, "admin "+action, err)
		return
	}
	s.audit(r.Context(), admin, admin.OrgID, "admin."+action, target.ID, "")
	done := map[string]string{"disable": "disabled", "enable": "enabled", "sign-out": "signed-out",
		"remove-codes": "codes-removed", "make-admin": "admin-added", "remove-admin": "admin-removed"}[action]
	http.Redirect(w, r, "/admin/people?done="+done, http.StatusSeeOther)
}

// ---- links ------------------------------------------------------------------------

func (s *Server) adminLinks(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	d := adminData{Title: "Links", Tab: "links", Email: admin.Email, Search: q.Get("q"), ShowAll: q.Get("all") == "1",
		Notice: adminNotices[q.Get("done")]}
	links, err := s.Store.ListOrgLinks(r.Context(), s.OrgID, store.LinkFilter{Search: d.Search, ActiveOnly: !d.ShowAll, Limit: 300}, s.Now())
	if err != nil {
		s.internalPage(w, "list links", err)
		return
	}
	d.Links = links
	s.renderAdmin(w, http.StatusOK, "admin-links", d)
}

func (s *Server) adminRevokeLink(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.RevokeOrgLink(r.Context(), admin.OrgID, id, s.Now()); err != nil {
		s.page(w, http.StatusNotFound, "Link not found", "It may have been removed.")
		return
	}
	s.audit(r.Context(), admin, admin.OrgID, "admin.link.revoke", id, "")
	back := url.Values{"done": {"revoked"}}
	if q := r.PostFormValue("q"); q != "" {
		back.Set("q", q)
	}
	if r.PostFormValue("all") == "1" {
		back.Set("all", "1")
	}
	http.Redirect(w, r, "/admin/links?"+back.Encode(), http.StatusSeeOther)
}

// ---- activity -------------------------------------------------------------------

const activityPage = 200

func (s *Server) adminActivity(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	events, err := s.Store.ListAudit(r.Context(), s.OrgID, before, activityPage)
	if err != nil {
		s.internalPage(w, "list activity", err)
		return
	}
	d := adminData{Title: "Activity", Tab: "activity", Email: admin.Email, Events: events}
	if len(events) == activityPage {
		d.OlderThan = events[len(events)-1].ID
	}
	s.renderAdmin(w, http.StatusOK, "admin-activity", d)
}

// describeAction turns an audit action into words for the activity page.
func describeAction(action string) string {
	if d, ok := map[string]string{
		"signin":                "signed in",
		"signin.failed":         "failed to sign in",
		"signin.refused":        "was refused (not a member)",
		"desktop.approve":       "approved a desktop sign-in",
		"desktop.signin":        "signed in on a computer",
		"upload.start":          "started an upload",
		"upload.complete":       "shared a file",
		"link.revoke":           "turned off their link",
		"link.download":         "downloaded a file",
		"password.set":          "set a password",
		"password.changed":      "changed their password",
		"passkey.added":         "added a passkey",
		"passkey.removed":       "removed a passkey",
		"passkey.clone_warning": "used a passkey that looks copied",
		"totp.enabled":          "turned on two-step codes",
		"totp.disabled":         "turned off two-step codes",
		"recovery_code.used":    "used a recovery code",
		"admin.invite":          "invited",
		"admin.reset":           "sent a sign-in reset to",
		"admin.disable":         "turned off",
		"admin.enable":          "turned on",
		"admin.sign-out":        "signed out everywhere:",
		"admin.remove-codes":    "removed two-step codes for",
		"admin.make-admin":      "made an admin:",
		"admin.granted":         "made an admin:",
		"admin.remove-admin":    "removed admin rights from",
		"admin.link.revoke":     "turned off a link",
		"admin.rules":           "changed the rules",
		"admin.rules.reset":     "reset the rules to the server's settings",
	}[action]; ok {
		return d
	}
	if strings.HasPrefix(action, "admin.") {
		return strings.ReplaceAll(strings.TrimPrefix(action, "admin."), "-", " ") + " (by admin)"
	}
	return action
}

func init() {
	pages.Funcs(template.FuncMap{
		"describe": describeAction,
		"when":     func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") },
		"size":     humanSize,
	})
	template.Must(pages.Parse(adminPages))
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	value, suffix := float64(n), []string{"KB", "MB", "GB", "TB"}
	i := -1
	for value >= unit && i < len(suffix)-1 {
		value /= unit
		i++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix[i]
}

const adminPages = `
{{define "admin-top"}}<div class="top">{{template "logo"}}<span><a class="btn quiet" href="/">Your links</a> <span class="who">{{.Email}}</span></span></div>
<nav class="tabs" aria-label="Admin">
<a href="/admin/people"{{if eq .Tab "people"}} aria-current="page"{{end}}>People</a>
<a href="/admin/links"{{if eq .Tab "links"}} aria-current="page"{{end}}>Links</a>
<a href="/admin/rules"{{if eq .Tab "rules"}} aria-current="page"{{end}}>Rules</a>
<a href="/admin/activity"{{if eq .Tab "activity"}} aria-current="page"{{end}}>Activity</a>
</nav>
{{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}
{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}
{{if .ShownLink}}<div class="linkbox"><label>Link to pass on <input readonly value="{{.ShownLink}}"></label></div>{{end}}{{end}}

{{define "admin-people"}}{{template "head" .}}<main class="wide admin">
{{template "admin-top" .}}
<h1>People</h1>
{{if .Local}}<form method="post" action="/admin/people/invite" class="inline-form">
<label>Invite by email <input name="email" type="email" required placeholder="name@example.com"></label>
<label class="check"><input type="checkbox" name="admin"> Admin</label>
<button class="btn primary" type="submit">Invite</button>
</form>
<p class="small">{{if .Mail}}The invite is emailed and works once, for 7 days.{{else}}Email isn't set up: you'll get a link to pass on yourself.{{end}}</p>
{{else}}<p class="small">Local accounts are off, so people join through the member list (FLING_MEMBER_EMAILS / FLING_MEMBER_DOMAINS) and appear here after their first sign-in.</p>{{end}}
<table>
<thead><tr><th>Account</th><th>Signs in with</th><th>Last sign-in</th><th><span class="sr">Actions</span></th></tr></thead>
<tbody>{{range .People}}<tr{{if .Disabled}} class="off"{{end}}>
<td><span class="name">{{.Email}}</span>{{if .IsSelf}} <span class="tag">you</span>{{end}}
<div class="sub">{{if .Local}}Local account{{else}}Member{{end}}{{if .IsAdmin}} · Admin{{if .AdminByConfig}} (setting){{end}}{{end}}{{if .Disabled}} · Turned off{{end}}</div></td>
<td class="sub">{{if .Passkeys}}{{.Passkeys}} passkey{{if gt .Passkeys 1}}s{{end}}{{end}}{{if and .Passkeys .PasswordHash}}, {{end}}{{if .PasswordHash}}password{{if .TOTPSecret}} + codes{{end}}{{end}}{{if not (or .Passkeys .PasswordHash)}}{{if .Local}}Not set up yet{{else}}Sign-in provider{{end}}{{end}}</td>
<td class="meta">{{if .LastLogin}}{{when .LastLogin}}{{else}}Never{{end}}</td>
<td class="actions">
{{if .Disabled}}<form method="post" action="/admin/people/{{.ID}}/enable"><button class="btn quiet" type="submit">Turn on</button></form>
{{else}}{{if and $.Local .Local}}<form method="post" action="/admin/people/{{.ID}}/reset"><button class="btn quiet" type="submit">Reset sign-in</button></form>{{end}}
<form method="post" action="/admin/people/{{.ID}}/sign-out"><button class="btn quiet" type="submit">Sign out everywhere</button></form>
{{if .TOTPSecret}}<form method="post" action="/admin/people/{{.ID}}/remove-codes"><button class="btn quiet" type="submit">Remove codes</button></form>{{end}}
{{if not .IsSelf}}{{if .Admin}}{{if not .AdminByConfig}}<form method="post" action="/admin/people/{{.ID}}/remove-admin"><button class="btn quiet" type="submit">Remove admin</button></form>{{end}}
{{else if not .AdminByConfig}}<form method="post" action="/admin/people/{{.ID}}/make-admin"><button class="btn quiet" type="submit">Make admin</button></form>{{end}}
<form method="post" action="/admin/people/{{.ID}}/disable"><button class="btn quiet" type="submit">Turn off</button></form>{{end}}{{end}}
</td></tr>{{else}}<tr><td colspan="4">Nobody yet.</td></tr>{{end}}</tbody>
</table>
</main></body></html>{{end}}

{{define "admin-links"}}{{template "head" .}}<main class="wide admin">
{{template "admin-top" .}}
<h1>Links</h1>
<form method="get" action="/admin/links" class="inline-form">
<label>Search <input name="q" type="search" value="{{.Search}}" placeholder="File name or email"></label>
<label class="check"><input type="checkbox" name="all" value="1"{{if .ShowAll}} checked{{end}}> Include expired and turned-off links</label>
<button class="btn" type="submit">Show</button>
</form>
<table>
<thead><tr><th>File</th><th>Shared by</th><th>Status</th><th><span class="sr">Actions</span></th></tr></thead>
<tbody>{{range .Links}}<tr>
<td><span class="name">{{.File.Name}}</span><div class="meta">{{size .File.Size}} · {{when .CreatedAt}} · {{downloads .DownloadCount}}</div></td>
<td class="sub">{{.OwnerEmail}}</td>
<td class="sub">{{if .RevokedAt}}Turned off{{else if $.Now.Before .ExpiresAt}}<span class="ok">Active</span> until {{when .ExpiresAt}}{{else}}Expired{{end}}</td>
<td class="actions">{{if and (not .RevokedAt) ($.Now.Before .ExpiresAt)}}<form method="post" action="/admin/links/{{.ID}}/revoke">
<input type="hidden" name="q" value="{{$.Search}}">{{if $.ShowAll}}<input type="hidden" name="all" value="1">{{end}}
<button class="btn quiet" type="submit">Turn off</button></form>{{end}}</td>
</tr>{{else}}<tr><td colspan="4">No links{{if .Search}} match "{{.Search}}"{{end}}.</td></tr>{{end}}</tbody>
</table>
<p class="small">Turning a link off stops it at once. {{if .Cleanup}}The gateway deletes a file from storage {{.Cleanup}} after its link was turned off or expired.{{else}}Files stay in storage until a rule on the bucket removes them (the gateway's own clean-up is off).{{end}}</p>
</main></body></html>{{end}}

{{define "admin-activity"}}{{template "head" .}}<main class="wide admin">
{{template "admin-top" .}}
<h1>Activity</h1>
<table>
<thead><tr><th>When (UTC)</th><th>What happened</th></tr></thead>
<tbody>{{range .Events}}<tr><td class="meta">{{when .At}}</td>
<td>{{if .ActorEmail}}<span class="name">{{.ActorEmail}}</span> {{end}}{{describe .Action}}{{if and .TargetEmail (ne .TargetEmail .ActorEmail)}} <span class="name">{{.TargetEmail}}</span>{{end}}</td></tr>
{{else}}<tr><td colspan="2">Nothing yet.</td></tr>{{end}}</tbody>
</table>
{{if .OlderThan}}<p><a class="btn quiet" href="/admin/activity?before={{.OlderThan}}">Older</a></p>{{end}}
</main></body></html>{{end}}
`
