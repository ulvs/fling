// Command fling-gateway serves the Fling Gateway: it signs S3 URLs for Fling clients and
// turns uploads into short, revocable links.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ulvs/fling/gateway/internal/api"
	"github.com/ulvs/fling/gateway/internal/auth"
	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/policy"
	"github.com/ulvs/fling/gateway/internal/storage"
	"github.com/ulvs/fling/gateway/internal/store"
)

const defaultOrgID = "default"

func main() {
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

	now := time.Now()
	if err := db.EnsureOrg(ctx, defaultOrgID, "Default", now); err != nil {
		return err
	}
	var devUser store.User
	if cfg.DevToken != "" {
		log.Warn("FLING_DEV_TOKEN is set: API calls with it act as the development user", "user", cfg.DevUserEmail)
		if devUser, err = db.EnsureUser(ctx, defaultOrgID, cfg.DevUserEmail, now); err != nil {
			return err
		}
	}

	objects, err := storage.NewS3(ctx, cfg.S3)
	if err != nil {
		return err
	}

	server := &api.Server{
		Store:     db,
		Objects:   objects,
		Policy:    policy.Policy(cfg.Policy),
		PublicURL: cfg.PublicURL,
		Log:       log,
		Now:       time.Now,
		Providers: auth.Providers(cfg, nil),
		Members:   cfg.Members,
		OrgID:     defaultOrgID,
		DevToken:  cfg.DevToken,
		DevUser:   devUser,
	}
	for _, p := range server.Providers {
		log.Info("sign-in provider", "provider", p.ID)
	}
	go cleanup(ctx, log, db)
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

// cleanup removes expired sessions and sign-ins in progress once an hour.
func cleanup(ctx context.Context, log *slog.Logger, db *store.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := db.DeleteExpired(ctx, now); err != nil {
				log.Warn("cleanup", "err", err)
			}
		}
	}
}
