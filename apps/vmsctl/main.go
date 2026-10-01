// Command vmsctl administers an OpenVMS installation from the command line.
//
//	vmsctl migrate                 apply database migrations
//	vmsctl bootstrap [-user admin] create the platform administrator and print its token
//	vmsctl sync-admin-permissions -user NAME add missing catalog grants without creating a token
//	vmsctl token -user NAME        issue another token for an existing user
//	vmsctl passwd -user NAME       set the login password of a user (read from stdin or -password)
//	vmsctl seed-demo               create the "demo" tenant against the compose Frigate mocks
//
// It reads DATABASE_URL and OPENVMS_MASTER_KEY (or OPENVMS_MASTER_KEY_FILE) like the API.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/platform/postgres"
	"github.com/jdolan-exalink/openvms/internal/secrets"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/migrations"
)

const usage = `usage: vmsctl <command> [flags]

commands:
  migrate      apply database migrations
  bootstrap    create the platform administrator and print its API token
  sync-admin-permissions  add missing catalog permissions to an existing platform user
  token        issue a new API token for an existing user
  passwd       set the login password of a user (stdin, or -password)
  seed-demo    create the demo tenant (sites, compose Frigate mocks, Operator-A, supervisor)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1], os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vmsctl:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, args []string, out io.Writer) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	switch cmd {
	case "migrate":
		v, err := postgres.Migrate(ctx, dbURL, migrations.FS, log)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "schema version %d\n", v)
		return nil
	case "bootstrap":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		user := fs.String("user", "admin", "platform administrator username")
		password := fs.String("password", "", "also set this login password (min. 10 characters)")
		_ = fs.Parse(args)
		st, closeFn, err := open(ctx, dbURL, log)
		if err != nil {
			return err
		}
		defer closeFn()
		_, token, err := bootstrap.PlatformAdmin(ctx, st, *user)
		if err != nil {
			return err
		}
		if *password != "" {
			if err := bootstrap.SetPassword(ctx, st, *user, *password); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "platform admin %q ready\ntoken: %s\n", *user, token)
		return nil
	case "sync-admin-permissions":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		user := fs.String("user", "", "existing platform username")
		_ = fs.Parse(args)
		if *user == "" {
			return errors.New("-user is required")
		}
		st, closeFn, err := open(ctx, dbURL, log)
		if err != nil {
			return err
		}
		defer closeFn()
		added, err := bootstrap.SyncPlatformAdmin(ctx, st, *user)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "platform permissions synchronized for %q (%d grants added; no token created)\n", *user, added)
		return nil
	case "token":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		user := fs.String("user", "", "username")
		ttl := fs.Duration("ttl", 0, "token lifetime (0 = no expiry)")
		_ = fs.Parse(args)
		if *user == "" {
			return errors.New("-user is required")
		}
		st, closeFn, err := open(ctx, dbURL, log)
		if err != nil {
			return err
		}
		defer closeFn()
		var token string
		err = st.Tx(ctx, store.AllTenants, func(q *db.Queries) error {
			u, err := q.GetUserByUsername(ctx, *user)
			if err != nil {
				return fmt.Errorf("user %q: %w", *user, store.Classify(err))
			}
			token, err = bootstrap.IssueToken(ctx, q, u.ID, "cli", *ttl)
			return err
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "token: %s\n", token)
		return nil
	case "passwd":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		user := fs.String("user", "", "username")
		password := fs.String("password", "", "new password (default: first line of stdin)")
		_ = fs.Parse(args)
		if *user == "" {
			return errors.New("-user is required")
		}
		if *password == "" {
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && line == "" {
				return errors.New("no password given: use -password or pipe it on stdin")
			}
			*password = strings.TrimRight(line, "\r\n")
		}
		st, closeFn, err := open(ctx, dbURL, log)
		if err != nil {
			return err
		}
		defer closeFn()
		if err := bootstrap.SetPassword(ctx, st, *user, *password); err != nil {
			return err
		}
		fmt.Fprintf(out, "password of %q updated\n", *user)
		return nil
	case "seed-demo":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		admin := fs.String("admin", "admin", "platform administrator that performs the seed")
		helvecia := fs.String("helvecia-url", "http://frigate-helvecia:8971", "Frigate of the Helvecia site")
		helveciaPass := fs.String("helvecia-password", "helvecia-dev", "")
		cayasta := fs.String("cayasta-url", "http://frigate-cayasta:8971", "Frigate of the Cayastá site")
		cayastaPass := fs.String("cayasta-password", "cayasta-dev", "")
		_ = fs.Parse(args)
		st, closeFn, err := open(ctx, dbURL, log)
		if err != nil {
			return err
		}
		defer closeFn()
		sealer, err := secrets.FromEnv()
		if err != nil {
			return err
		}
		actor, _, err := bootstrap.PlatformAdmin(ctx, st, *admin)
		if err != nil {
			return err
		}
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		res, err := bootstrap.Demo(sctx, inventory.New(st, sealer, log), actor, []bootstrap.DemoServer{
			{Site: "Helvecia", Name: "frigate-h01", BaseURL: *helvecia, Username: "admin", Password: *helveciaPass},
			{Site: "Cayastá", Name: "frigate-c01", BaseURL: *cayasta, Username: "admin", Password: *cayastaPass},
		}, []string{"frigate-h01/acceso_norte", "frigate-c01/muelle"})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "tenant demo created (%s)\n", res.TenantID)
		names := make([]string, 0, len(res.Tokens))
		for n := range res.Tokens {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(out, "%-11s token: %s\n", n, res.Tokens[n])
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, strings.TrimSpace(usage))
	}
}

func open(ctx context.Context, url string, log *slog.Logger) (*store.Store, func(), error) {
	if _, err := postgres.Migrate(ctx, url, migrations.FS, log); err != nil {
		return nil, nil, err
	}
	pool, err := postgres.Connect(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	return &store.Store{Pool: pool}, pool.Close, nil
}
