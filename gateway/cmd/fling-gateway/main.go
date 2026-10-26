// Command fling-gateway serves the Fling Gateway: it signs S3 URLs for Fling clients and
// turns uploads into short, revocable links.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ulvs/fling/gateway/internal/api"
	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/mail"
	"github.com/ulvs/fling/gateway/internal/policy"
	"github.com/ulvs/fling/gateway/internal/storage"
	"github.com/ulvs/fling/gateway/internal/store"
)

const defaultOrgID = "default"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "user" {
		os.Exit(userCommand(os.Args[2:], os.Stdout, os.Stderr))
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fling-gateway stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	server, err := newServer(ctx, cfg, log, db)
	if err != nil {
		return err
	}
	if cfg.DevToken != "" {
		log.Warn("FLING_DEV_TOKEN is set: API calls with it act as the development user", "user", cfg.DevUserEmail)
	}
	for _, p := range server.Providers {
		log.Info("sign-in provider", "provider", p.ID)
	}
	if server.Local {
		log.Info("local accounts on", "passkeys", true, "two_factor_required", cfg.RequireTwoFactor, "mail", cfg.Mail.Enabled())
	}
	go cleanup(ctx, log, db, server)
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Listen, "public_url", cfg.PublicURL)
		errs <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}

// cleanup runs once a minute after start and then every hour: it removes expired sessions and
// sign-ins in progress, and deletes files nobody can download any more.
func cleanup(ctx context.Context, log *slog.Logger, db *store.Store, server *api.Server) {
	warnedDenied := false
	run := func(now time.Time) {
		if err := db.DeleteExpired(ctx, now); err != nil {
			log.Warn("cleanup", "err", err)
		}
		_, err := server.DeleteExpiredFiles(ctx)
		switch {
		case errors.Is(err, storage.ErrAccessDenied):
			if !warnedDenied {
				warnedDenied = true
				log.Warn("the storage key may not delete files, so expired files stay in the bucket: allow s3:DeleteObject, or set FLING_DELETE_EXPIRED_FILES=false and use a lifecycle rule")
			}
		case err != nil:
			log.Warn("deleting expired files", "err", err)
		}
	}
	first := time.NewTimer(time.Minute)
	defer first.Stop()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-first.C:
			run(now)
		case now := <-ticker.C:
			run(now)
		}
	}
}

// newServer builds the gateway from its configuration; the user command uses it too.
func newServer(ctx context.Context, cfg config.Config, log *slog.Logger, db *store.Store) (*api.Server, error) {
	now := time.Now()
	if err := db.EnsureOrg(ctx, defaultOrgID, "Default", now); err != nil {
		return nil, err
	}
	var devUser store.User
	if cfg.DevToken != "" {
		var err error
		if devUser, err = db.EnsureUser(ctx, defaultOrgID, cfg.DevUserEmail, now); err != nil {
			return nil, err
		}
	}
	objects, err := storage.NewS3(ctx, cfg.S3)
	if err != nil {
		return nil, err
	}
	server := &api.Server{
		Store:            db,
		Objects:          objects,
		Policy:           policy.Policy{DefaultTTL: cfg.Policy.DefaultTTL, MaxTTL: cfg.Policy.MaxTTL, MaxUploadBytes: cfg.Policy.MaxUploadBytes},
		PublicURL:        cfg.PublicURL,
		Log:              log,
		Now:              time.Now,
		Providers:        auth.Providers(cfg, nil),
		Members:          cfg.Members,
		Admins:           cfg.Admins,
		OrgID:            defaultOrgID,
		Local:            cfg.LocalAccounts,
		RequireTwoFactor: cfg.RequireTwoFactor,
		TrustedProxies:   cfg.TrustedProxies,
		DeleteFiles:      cfg.Cleanup.Enabled,
		DeleteAfter:      cfg.Cleanup.After,
		DevToken:         cfg.DevToken,
		DevUser:          devUser,
	}
	if cfg.LocalAccounts {
		u, _ := url.Parse(cfg.PublicURL)
		if server.WebAuthn, err = api.NewWebAuthn(cfg.PublicURL, u.Hostname()); err != nil {
			return nil, fmt.Errorf("passkeys: %w", err)
		}
	}
	if cfg.Mail.Enabled() {
		server.Mail = mail.NewResend(cfg.Mail.ResendAPIKey, cfg.Mail.From)
	}
	return server, nil
}
