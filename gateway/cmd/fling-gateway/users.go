package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ulvs/fling/gateway/internal/api"
	"github.com/ulvs/fling/gateway/internal/config"
	"github.com/ulvs/fling/gateway/internal/store"
)

const userUsage = `Manage local accounts (FLING_LOCAL_ACCOUNTS=true). In Docker:
  docker exec <container> fling-gateway user <command>

Commands:
  invite <email>     Create an account and make an invite link (valid 7 days).
                     The link is emailed when Resend is set up, and always printed.
  reset <email>      Make a link to choose a new password or passkey (valid 24 hours).
  disable <email>    Turn the account off and sign it out everywhere.
  enable <email>     Turn it back on.
  remove-codes <email>
                     Turn off two-step codes, e.g. when someone lost their phone and recovery codes.
  list               List accounts.
`

// userCommand runs "fling-gateway user ..." against the gateway's database and returns the exit code.
func userCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, userUsage)
		return 0
	}
	cfg, err := config.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, "configuration:", err)
		return 2
	}
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer db.Close()
	server, err := newServer(ctx, cfg, slog.New(slog.NewTextHandler(stderr, nil)), db)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := runUserCommand(ctx, server, cfg, args, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runUserCommand(ctx context.Context, s *api.Server, cfg config.Config, args []string, out io.Writer) error {
	cmd := args[0]
	if cmd == "list" {
		return listAccounts(ctx, s, out)
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: fling-gateway user %s <email>", cmd)
	}
	email := strings.ToLower(strings.TrimSpace(args[1]))
	if !strings.Contains(email, "@") {
		return fmt.Errorf("%q isn't an email address", args[1])
	}
	now := s.Now()
	switch cmd {
	case "invite", "reset":
		if !cfg.LocalAccounts {
			return fmt.Errorf("local accounts are off: set FLING_LOCAL_ACCOUNTS=true first")
		}
		var user store.User
		if cmd == "invite" {
			u, err := s.Store.EnsureLocalUser(ctx, s.OrgID, email, now)
			if err != nil {
				return err
			}
			user = u
		} else {
			a, err := s.Store.AccountByEmail(ctx, s.OrgID, email)
			if err != nil || !a.Local {
				return fmt.Errorf("there's no local account for %s; use invite", email)
			}
			user = a.User
		}
		if user.Disabled {
			return fmt.Errorf("%s is turned off; run enable first", email)
		}
		ttl := api.InviteTTL
		if cmd == "reset" {
			ttl = api.ResetTTL
		}
		link, sent, err := s.CreateAccountLink(ctx, user, cmd, ttl)
		if link == "" {
			return err
		}
		switch {
		case sent:
			fmt.Fprintf(out, "Emailed the link to %s.\n", email)
		case err != nil:
			fmt.Fprintf(out, "Couldn't email the link (%v). Pass it on yourself:\n", err)
		default:
			fmt.Fprintf(out, "Send this link to %s yourself (email isn't set up):\n", email)
		}
		fmt.Fprintf(out, "%s\nIt works once and expires %s.\n", link, now.Add(ttl).UTC().Format("2006-01-02 15:04 UTC"))
		return nil
	}

	a, err := s.Store.AccountByEmail(ctx, s.OrgID, email)
	if err != nil {
		return fmt.Errorf("there's no account for %s", email)
	}
	switch cmd {
	case "disable":
		err = s.Store.SetDisabled(ctx, a.ID, true, now)
	case "enable":
		err = s.Store.SetDisabled(ctx, a.ID, false, now)
	case "remove-codes":
		err = s.Store.DisableTOTP(ctx, a.ID)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, userUsage)
	}
	if err != nil {
		return err
	}
	s.Store.Audit(ctx, store.AuditEvent{OrgID: a.OrgID, At: now, Action: "admin." + cmd, Target: a.ID, Detail: "via cli"})
	fmt.Fprintf(out, "Done: %s %s.\n", cmd, email)
	return nil
}

func listAccounts(ctx context.Context, s *api.Server, out io.Writer) error {
	accounts, err := s.Store.ListAccounts(ctx, s.OrgID)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tTYPE\tSTATE\tPASSWORD\tCODES\tPASSKEYS\tLAST SIGN-IN")
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "-"
	}
	for _, a := range accounts {
		kind, state, last := "member", "on", "never"
		if a.Local {
			kind = "local"
		}
		if a.Disabled {
			state = "off"
		}
		if a.LastLogin != nil {
			last = a.LastLogin.UTC().Format(time.DateOnly)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", a.Email, kind, state, yesNo(a.PasswordHash != ""), yesNo(a.TOTPSecret != ""), a.Passkeys, last)
	}
	return tw.Flush()
}
