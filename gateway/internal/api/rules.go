package api

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ulvs/fling/gateway/internal/policy"
	"github.com/ulvs/fling/gateway/internal/store"
)

const day = 24 * time.Hour

// policyFor is the server's rules (s.Policy) with the organisation's overrides on top.
func (s *Server) policyFor(ctx context.Context, orgID string) (policy.Policy, error) {
	p := s.Policy
	r, found, err := s.Store.OrgRules(ctx, orgID)
	if err != nil || !found {
		return p, err
	}
	if r.DefaultTTL != nil {
		p.DefaultTTL = *r.DefaultTTL
	}
	if r.MaxTTL != nil {
		p.MaxTTL = *r.MaxTTL
	}
	if r.MaxUploadBytes != nil {
		p.MaxUploadBytes = *r.MaxUploadBytes
	}
	p.BlockedTypes = policy.ParseTypes(r.BlockedTypes)
	p.AllowedTypes = policy.ParseTypes(r.AllowedTypes)
	return p, nil
}

type rulesForm struct {
	DefaultDays, MaxDays, MaxMB string
	Blocked, Allowed            string
}

type rulesData struct {
	adminData
	Form                   rulesForm
	ServerDefaultDays      string
	ServerMaxDays          string
	ServerMaxMB            string
	Effective              policy.Policy
	UpdatedAt              time.Time
	UpdatedBy              string
	Custom                 bool
	HardMaxDays, HardMaxMB int
}

func days(d time.Duration) string {
	if d%day == 0 {
		return strconv.FormatInt(int64(d/day), 10)
	}
	return strconv.FormatFloat(d.Hours()/24, 'f', 1, 64)
}

func megabytes(n int64) string { return strconv.FormatInt(n>>20, 10) }

func (s *Server) adminRules(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	s.renderRules(w, r, admin, http.StatusOK, nil, adminData{Notice: map[string]string{
		"saved": "Rules saved. They apply to new uploads.", "reset": "Back to the server's settings.",
	}[r.URL.Query().Get("done")]})
}

// renderRules shows the page; form, when not nil, is what the admin typed (after an error).
func (s *Server) renderRules(w http.ResponseWriter, r *http.Request, admin store.User, status int, form *rulesForm, base adminData) {
	stored, found, err := s.Store.OrgRules(r.Context(), s.OrgID)
	if err != nil {
		s.internalPage(w, "load rules", err)
		return
	}
	effective, err := s.policyFor(r.Context(), s.OrgID)
	if err != nil {
		s.internalPage(w, "load rules", err)
		return
	}
	d := rulesData{adminData: base, Effective: effective, Custom: found, UpdatedAt: stored.UpdatedAt, UpdatedBy: stored.UpdatedBy,
		ServerDefaultDays: days(s.Policy.DefaultTTL), ServerMaxDays: days(s.Policy.MaxTTL), ServerMaxMB: megabytes(s.Policy.MaxUploadBytes),
		HardMaxDays: int(policy.MaxLinkTTL / day), HardMaxMB: policy.MaxSingleUpload >> 20}
	d.Title, d.Tab, d.Email = "Rules", "rules", admin.Email
	if form != nil {
		d.Form = *form
	} else {
		if stored.DefaultTTL != nil {
			d.Form.DefaultDays = days(*stored.DefaultTTL)
		}
		if stored.MaxTTL != nil {
			d.Form.MaxDays = days(*stored.MaxTTL)
		}
		if stored.MaxUploadBytes != nil {
			d.Form.MaxMB = megabytes(*stored.MaxUploadBytes)
		}
		d.Form.Blocked = strings.Join(policy.ParseTypes(stored.BlockedTypes), ", ")
		d.Form.Allowed = strings.Join(policy.ParseTypes(stored.AllowedTypes), ", ")
	}
	d.Now, d.PublicURL = s.Now(), s.PublicURL
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pages.ExecuteTemplate(w, "admin-rules", d); err != nil {
		s.Log.Error("render rules page", "err", err)
	}
}

func (s *Server) adminSaveRules(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	form := rulesForm{
		DefaultDays: strings.TrimSpace(r.PostFormValue("default_days")),
		MaxDays:     strings.TrimSpace(r.PostFormValue("max_days")),
		MaxMB:       strings.TrimSpace(r.PostFormValue("max_mb")),
		Blocked:     r.PostFormValue("blocked"),
		Allowed:     r.PostFormValue("allowed"),
	}
	problem := func(msg string) {
		s.renderRules(w, r, admin, http.StatusUnprocessableEntity, &form, adminData{Problem: msg})
	}
	whole := func(raw string, lo, hi int64, what string) (*int64, bool) {
		if raw == "" {
			return nil, true
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < lo || n > hi {
			problem(fmt.Sprintf("%s must be a whole number from %d to %d, or empty for the server's setting.", what, lo, hi))
			return nil, false
		}
		return &n, true
	}
	hardDays := int64(policy.MaxLinkTTL / day)
	defDays, ok1 := whole(form.DefaultDays, 1, hardDays, "Default link lifetime")
	if !ok1 {
		return
	}
	maxDays, ok2 := whole(form.MaxDays, 1, hardDays, "Longest link lifetime")
	if !ok2 {
		return
	}
	maxMB, ok3 := whole(form.MaxMB, 1, policy.MaxSingleUpload>>20, "Largest file")
	if !ok3 {
		return
	}

	var rules store.Rules
	if defDays != nil {
		d := time.Duration(*defDays) * day
		rules.DefaultTTL = &d
	}
	if maxDays != nil {
		d := time.Duration(*maxDays) * day
		rules.MaxTTL = &d
	}
	if maxMB != nil {
		b := *maxMB << 20
		rules.MaxUploadBytes = &b
	}
	effDefault, effMax := s.Policy.DefaultTTL, s.Policy.MaxTTL
	if rules.DefaultTTL != nil {
		effDefault = *rules.DefaultTTL
	}
	if rules.MaxTTL != nil {
		effMax = *rules.MaxTTL
	}
	if effMax < effDefault {
		problem("The longest link lifetime can't be shorter than the default.")
		return
	}
	rules.BlockedTypes = strings.Join(policy.ParseTypes(form.Blocked), ", ")
	rules.AllowedTypes = strings.Join(policy.ParseTypes(form.Allowed), ", ")

	if err := s.Store.SaveOrgRules(r.Context(), s.OrgID, rules, admin.ID, s.Now()); err != nil {
		s.internalPage(w, "save rules", err)
		return
	}
	s.audit(r.Context(), admin, admin.OrgID, "admin.rules", "",
		fmt.Sprintf("default=%s max=%s size=%s blocked=%q allowed=%q", form.DefaultDays, form.MaxDays, form.MaxMB, rules.BlockedTypes, rules.AllowedTypes))
	http.Redirect(w, r, "/admin/rules?done=saved", http.StatusSeeOther)
}

func (s *Server) adminResetRules(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminUser(w, r)
	if !ok {
		return
	}
	if err := s.Store.ResetOrgRules(r.Context(), s.OrgID); err != nil {
		s.internalPage(w, "reset rules", err)
		return
	}
	s.audit(r.Context(), admin, admin.OrgID, "admin.rules.reset", "", "")
	http.Redirect(w, r, "/admin/rules?done=reset", http.StatusSeeOther)
}

func init() {
	pages.Funcs(template.FuncMap{
		"days":      days,
		"megabytes": megabytes,
		"joinTypes": func(t []string) string { return strings.Join(t, ", ") },
	})
	template.Must(pages.Parse(rulesPage))
}

const rulesPage = `
{{define "admin-rules"}}{{template "head" .}}<main class="wide admin">
{{template "admin-top" .}}
<h1>Rules</h1>
<p>These apply to every upload in this organisation. Leave a limit empty to use the server's setting.</p>
<form method="post" action="/admin/rules" class="stack rules">
<label>Default link lifetime (days) <input name="default_days" type="number" min="1" max="{{.HardMaxDays}}" value="{{.Form.DefaultDays}}" placeholder="{{.ServerDefaultDays}} (server setting)"></label>
<p class="small">Used when Fling doesn't ask for a lifetime.</p>
<label>Longest link lifetime (days) <input name="max_days" type="number" min="1" max="{{.HardMaxDays}}" value="{{.Form.MaxDays}}" placeholder="{{.ServerMaxDays}} (server setting)"></label>
<p class="small">Longer requests are shortened to this. Check that your storage keeps files at least this long: an expiry rule on the bucket deletes files even while their link is still valid.</p>
<label>Largest file (MB) <input name="max_mb" type="number" min="1" max="{{.HardMaxMB}}" value="{{.Form.MaxMB}}" placeholder="{{.ServerMaxMB}} (server setting)"></label>
<p class="small">Up to {{.HardMaxMB}} MB for now. Bigger files come with uploads in parts.</p>
<label>Blocked file types <input name="blocked" value="{{.Form.Blocked}}" placeholder=".exe, .bat, .msi"></label>
<label>Only allow these file types <input name="allowed" value="{{.Form.Allowed}}" placeholder="Leave empty to allow everything not blocked"></label>
<p class="small">Fling sends several files or a folder as one .zip, so add .zip if you only allow some types.</p>
<div><button class="btn primary" type="submit">Save rules</button></div>
</form>
<section style="margin-top:24px"><h2>In effect now</h2>
<p class="sub">Links last {{days .Effective.DefaultTTL}} days by default and at most {{days .Effective.MaxTTL}} days. Files up to {{megabytes .Effective.MaxUploadBytes}} MB.
{{if .Effective.BlockedTypes}} Blocked: {{joinTypes .Effective.BlockedTypes}}.{{end}}{{if .Effective.AllowedTypes}} Only: {{joinTypes .Effective.AllowedTypes}}.{{end}}
{{if .Custom}} Last changed {{when .UpdatedAt}} UTC{{if .UpdatedBy}} by {{.UpdatedBy}}{{end}}.{{end}}</p>
{{if .Custom}}<form method="post" action="/admin/rules/reset"><button class="btn quiet" type="submit">Go back to the server's settings</button></form>{{end}}
</section>
</main></body></html>{{end}}
`
