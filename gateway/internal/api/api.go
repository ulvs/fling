// Package api serves the gateway's HTTP API and public download links.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/mail"
	"github.com/ulvs/fling/gateway/internal/policy"
	"github.com/ulvs/fling/gateway/internal/storage"
	"github.com/ulvs/fling/gateway/internal/store"
)

const (
	uploadURLTTL   = 15 * time.Minute
	downloadURLTTL = 5 * time.Minute
	maxJSONBody    = 64 << 10
	maxNameLength  = 255
)

type Server struct {
	Store     *store.Store
	Objects   storage.Objects
	Policy    policy.Policy
	PublicURL string
	Log       *slog.Logger
	Now       func() time.Time

	// Sign-in: the configured providers, who may sign in, and the organisation new members join.
	Providers []*auth.Provider
	Members   Members
	Admins    Members // FLING_ADMIN_EMAILS
	OrgID     string

	// Deleting files nobody can download any more (FLING_DELETE_EXPIRED_FILES, FLING_DELETE_AFTER).
	DeleteFiles bool
	DeleteAfter time.Duration

	// Local accounts: invited users with passkeys, or passwords plus optional two-step codes.
	Local            bool
	RequireTwoFactor bool
	WebAuthn         *webauthn.WebAuthn // set when Local is on
	Mail             mail.Sender        // nil: links are handed out by the admin
	TrustedProxies   []netip.Prefix

	// Failed sign-in attempts, per email address and per client IP address.
	byEmail *auth.Throttle
	byIP    *auth.Throttle

	// DevToken and DevUser authenticate API calls without signing in, for development.
	DevToken string
	DevUser  store.User
}

// Members decides who may sign in, by verified email address.
type Members interface{ Allows(email string) bool }

type ctxKey struct{}

// tokenKey carries the hash of the desktop token that authenticated a request.
type tokenKey struct{}

func (s *Server) Handler() http.Handler {
	if s.byEmail == nil {
		s.byEmail = auth.NewThrottle(5, 15*time.Minute)
		s.byIP = auth.NewThrottle(30, 15*time.Minute)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /auth/login/{provider}", s.login)
	mux.HandleFunc("GET /auth/callback/{provider}", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /auth/desktop", s.desktopStart)
	mux.HandleFunc("GET /auth/desktop/{flow}", s.desktopConfirm)
	mux.HandleFunc("POST /auth/desktop/{flow}", s.desktopApprove)
	mux.HandleFunc("POST /auth/password", s.passwordSignIn)
	mux.HandleFunc("GET /auth/code", s.codePage)
	mux.HandleFunc("POST /auth/code", s.codeSubmit)
	mux.HandleFunc("GET /auth/code/setup", s.codeSetupPage)
	mux.HandleFunc("POST /auth/code/setup", s.codeSetupSubmit)
	mux.HandleFunc("GET /auth/link/{token}", s.linkPage)
	mux.HandleFunc("POST /auth/link/{token}", s.linkSubmit)
	mux.HandleFunc("GET /auth/forgot", s.forgotPage)
	mux.HandleFunc("POST /auth/forgot", s.forgotSubmit)
	mux.HandleFunc("POST /auth/passkey/register/begin", s.passkeyRegisterBegin)
	mux.HandleFunc("POST /auth/passkey/register/finish", s.passkeyRegisterFinish)
	mux.HandleFunc("POST /auth/passkey/login/begin", s.passkeyLoginBegin)
	mux.HandleFunc("POST /auth/passkey/login/finish", s.passkeyLoginFinish)
	mux.HandleFunc("GET /account", s.accountPage)
	mux.HandleFunc("POST /account/password", s.changePassword)
	mux.HandleFunc("POST /account/code/off", s.turnOffCodes)
	mux.HandleFunc("POST /account/passkeys/{id}/remove", s.removePasskey)
	mux.HandleFunc("GET /static/fling.js", s.script)
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/people", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/people", s.adminPeople)
	mux.HandleFunc("POST /admin/people/invite", s.adminInvite)
	mux.HandleFunc("POST /admin/people/{id}/{action}", s.adminPersonAction)
	mux.HandleFunc("GET /admin/links", s.adminLinks)
	mux.HandleFunc("POST /admin/links/{id}/revoke", s.adminRevokeLink)
	mux.HandleFunc("GET /admin/activity", s.adminActivity)
	mux.HandleFunc("GET /admin/rules", s.adminRules)
	mux.HandleFunc("POST /admin/rules", s.adminSaveRules)
	mux.HandleFunc("POST /admin/rules/reset", s.adminResetRules)
	mux.HandleFunc("POST /api/v1/token", s.token)
	mux.Handle("GET /api/v1/me", s.auth(s.me))
	mux.Handle("DELETE /api/v1/session", s.auth(s.signOut))
	mux.Handle("POST /api/v1/uploads", s.auth(s.createUpload))
	mux.Handle("POST /api/v1/uploads/{id}/complete", s.auth(s.completeUpload))
	mux.Handle("GET /api/v1/links", s.auth(s.listLinks))
	mux.Handle("DELETE /api/v1/links/{id}", s.auth(s.revokeLink))
	mux.HandleFunc("GET /l/{slug}", s.download)
	return s.logRequests(securityHeaders(mux))
}

// ---- middleware -------------------------------------------------------------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests logs the route pattern, never the raw path: link slugs are secrets.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.Pattern == "GET /healthz" {
			return
		}
		s.Log.Info("request", "route", r.Pattern, "status", rec.status, "ms", time.Since(started).Milliseconds())
	})
}

// auth accepts a desktop token from the token endpoint, or the development token.
func (s *Server) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			s.unauthorized(w)
			return
		}
		if s.DevToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.DevToken)) == 1 {
			next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, s.DevUser)))
			return
		}
		now := s.Now()
		hash := store.HashToken(token)
		se, err := s.Store.SessionByHash(r.Context(), hash, now)
		if errors.Is(err, store.ErrNotFound) || (err == nil && (se.Kind != "desktop" || !s.allowed(se.User))) {
			s.unauthorized(w)
			return
		} else if err != nil {
			s.internalError(w, "session lookup", err)
			return
		}
		if now.Sub(se.LastSeen) >= touchInterval {
			if err := s.Store.TouchSession(r.Context(), hash, now.Add(desktopSessionTTL), now); err != nil {
				s.Log.Warn("touch session", "err", err)
			}
		}
		ctx := context.WithValue(context.WithValue(r.Context(), ctxKey{}, se.User), tokenKey{}, hash)
		next(w, r.WithContext(ctx))
	})
}

// clientIP is the caller's address. Behind trusted proxies it's the last X-Forwarded-For hop
// that isn't a trusted proxy itself; anything a client writes further left is ignored.
func (s *Server) clientIP(r *http.Request) string {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	ip := ap.Addr().Unmap()
	if !s.trustedProxy(ip) {
		return ip.String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		ip = hop.Unmap()
		if !s.trustedProxy(ip) {
			break
		}
	}
	return ip.String()
}

func (s *Server) trustedProxy(ip netip.Addr) bool {
	for _, p := range s.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="fling"`)
	writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to Fling Gateway first.")
}

func userFrom(r *http.Request) store.User { return r.Context().Value(ctxKey{}).(store.User) }

// ---- JSON helpers -----------------------------------------------------------

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "The request body isn't valid JSON for this call.")
		return false
	}
	return true
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	s.Log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Something went wrong on the gateway.")
}

func (s *Server) audit(ctx context.Context, user store.User, orgID, action, target, detail string) {
	err := s.Store.Audit(ctx, store.AuditEvent{OrgID: orgID, At: s.Now(), ActorID: user.ID, Action: action, Target: target, Detail: detail})
	if err != nil {
		s.Log.Error("audit write failed", "action", action, "err", err)
	}
}

// ---- handlers ---------------------------------------------------------------

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.Objects.Ping(ctx); err != nil {
		s.Log.Warn("storage not reachable", "err", err)
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ready"))
}

type createUploadRequest struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	TTLSeconds  int64  `json:"ttlSeconds"`
}

type uploadTarget struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type createUploadResponse struct {
	UploadID  string       `json:"uploadId"`
	Upload    uploadTarget `json:"upload"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	var req createUploadRequest
	if !readJSON(w, r, &req) {
		return
	}
	name, ok := cleanName(req.Name)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "bad_name", "The file name is empty or too long.")
		return
	}
	pol, err := s.policyFor(r.Context(), user.OrgID)
	if err != nil {
		s.internalError(w, "load rules", err)
		return
	}
	if err := pol.CheckFile(name, req.Size); err != nil {
		var v *policy.Violation
		if errors.As(err, &v) {
			writeError(w, http.StatusUnprocessableEntity, v.Code, v.Message)
			return
		}
		s.internalError(w, "policy check", err)
		return
	}
	contentType := req.ContentType
	if _, _, err := mime.ParseMediaType(contentType); err != nil || contentType == "" {
		contentType = "application/octet-stream"
	}

	now := s.Now()
	f := store.File{
		ID:          store.NewID(),
		OrgID:       user.OrgID,
		OwnerID:     user.ID,
		Name:        name,
		Size:        req.Size,
		ContentType: contentType,
		LinkTTL:     pol.LinkTTL(time.Duration(req.TTLSeconds) * time.Second),
		CreatedAt:   now,
	}
	f.ObjectKey = user.OrgID + "/" + f.ID + "/" + name

	url, headers, err := s.Objects.PresignPut(r.Context(), f.ObjectKey, contentType, f.Size, uploadURLTTL)
	if err != nil {
		s.internalError(w, "presign put", err)
		return
	}
	if err := s.Store.CreateFile(r.Context(), f); err != nil {
		s.internalError(w, "create file", err)
		return
	}
	s.audit(r.Context(), user, user.OrgID, "upload.start", f.ID, fmt.Sprintf("size=%d", f.Size))

	flat := map[string]string{}
	for k := range headers {
		flat[k] = headers.Get(k)
	}
	writeJSON(w, http.StatusCreated, createUploadResponse{
		UploadID:  f.ID,
		Upload:    uploadTarget{Method: http.MethodPut, URL: url, Headers: flat},
		ExpiresAt: now.Add(uploadURLTTL).UTC(),
	})
}

type linkResponse struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	Downloads int64      `json:"downloads"`
}

func (s *Server) toLinkResponse(l store.LinkWithFile) linkResponse {
	resp := linkResponse{
		ID: l.ID, URL: s.PublicURL + "/l/" + l.Slug, Name: l.File.Name, Size: l.File.Size,
		CreatedAt: l.CreatedAt.UTC(), ExpiresAt: l.ExpiresAt.UTC(), Downloads: l.DownloadCount,
	}
	if l.RevokedAt != nil {
		t := l.RevokedAt.UTC()
		resp.RevokedAt = &t
	}
	return resp
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	f, err := s.Store.GetFile(r.Context(), user.OrgID, user.ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "There's no such upload.")
		return
	} else if err != nil {
		s.internalError(w, "get file", err)
		return
	}
	if f.State != "pending" {
		writeError(w, http.StatusConflict, "already_complete", "This upload is already finished.")
		return
	}

	size, err := s.Objects.Size(r.Context(), f.ObjectKey)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusConflict, "not_uploaded", "The file hasn't reached the storage yet.")
		return
	} else if err != nil {
		s.internalError(w, "stat object", err)
		return
	}
	if size != f.Size {
		writeError(w, http.StatusConflict, "size_mismatch", "The stored file doesn't match the announced size.")
		return
	}

	now := s.Now()
	link := store.Link{ID: store.NewID(), OrgID: f.OrgID, Slug: store.NewSlug(), ExpiresAt: now.Add(f.LinkTTL), CreatedAt: now}
	if err := s.Store.CompleteFile(r.Context(), f.ID, link, now); err != nil {
		s.internalError(w, "complete file", err)
		return
	}
	s.audit(r.Context(), user, f.OrgID, "upload.complete", f.ID, "link="+link.ID)

	lf := store.LinkWithFile{Link: link, File: f}
	lf.FileID = f.ID
	writeJSON(w, http.StatusOK, s.toLinkResponse(lf))
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	links, err := s.Store.ListLinks(r.Context(), user.OrgID, user.ID)
	if err != nil {
		s.internalError(w, "list links", err)
		return
	}
	out := make([]linkResponse, 0, len(links))
	for _, l := range links {
		out = append(out, s.toLinkResponse(l))
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (s *Server) revokeLink(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	id := r.PathValue("id")
	err := s.Store.RevokeLink(r.Context(), user.OrgID, user.ID, id, s.Now())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "There's no such link.")
		return
	} else if err != nil {
		s.internalError(w, "revoke link", err)
		return
	}
	s.audit(r.Context(), user, user.OrgID, "link.revoke", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	l, err := s.Store.LinkBySlug(r.Context(), r.PathValue("slug"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.page(w, http.StatusNotFound, "Link not found", "Check that you copied the whole link.")
		return
	case err != nil:
		s.Log.Error("link lookup", "err", err)
		s.page(w, http.StatusInternalServerError, "Something went wrong", "Try again in a moment.")
		return
	case l.RevokedAt != nil:
		s.page(w, http.StatusGone, "This link was turned off", "Ask the person who shared it for a new one.")
		return
	case !s.Now().Before(l.ExpiresAt):
		s.page(w, http.StatusGone, "This link has expired", "Ask the person who shared it for a new one.")
		return
	}

	url, err := s.Objects.PresignGet(r.Context(), l.File.ObjectKey, l.File.Name, downloadURLTTL)
	if err != nil {
		s.Log.Error("presign get", "err", err)
		s.page(w, http.StatusBadGateway, "The file isn't reachable right now", "Try again in a moment.")
		return
	}
	if err := s.Store.CountDownload(r.Context(), l.ID); err != nil {
		s.Log.Error("count download", "err", err)
	}
	s.audit(r.Context(), store.User{}, l.OrgID, "link.download", l.ID, "")
	http.Redirect(w, r, url, http.StatusFound)
}

// cleanName keeps only the base name, without control characters or path separators.
func cleanName(raw string) (string, bool) {
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, path.Base(strings.ReplaceAll(raw, "\\", "/")))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || utf8.RuneCountInString(name) > maxNameLength {
		return "", false
	}
	return name, true
}
