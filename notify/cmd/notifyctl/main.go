// Command notifyctl is a small operator CLI for the notify module: apply the
// schema, emit a test event, and dump the recent admin feed. It is a thin
// wrapper over the library; all logic lives in package notify.
//
// The database DSN is read from NOTIFY_DATABASE_URL, falling back to
// DATABASE_URL.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/notify"
	"github.com/infodancer/web/notify/admin"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "notifyctl:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageErr()
	}

	dsn := firstNonEmpty(os.Getenv("NOTIFY_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if dsn == "" {
		return fmt.Errorf("set NOTIFY_DATABASE_URL or DATABASE_URL")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	switch args[0] {
	case "migrate":
		if err := notify.Migrate(ctx, pool); err != nil {
			return err
		}
		fmt.Println("migrations applied")
		return nil

	case "emit-test":
		id, err := notify.NewStore(pool).Insert(ctx, notify.Event{
			Type:     "user.created",
			Source:   "notifyctl",
			Title:    "Test event from notifyctl",
			Severity: notify.SeverityNotice,
			Actor:    &notify.Identity{Issuer: "notifyctl", Subject: "tester", Label: "tester@local"},
			Payload:  map[string]any{"emitted_at": time.Now().UTC().Format(time.RFC3339)},
		})
		if err != nil {
			return err
		}
		fmt.Printf("inserted event id=%d\n", id)
		return nil

	case "feed-dump":
		feed, err := notify.NewStore(pool).AdminFeed(ctx, notify.Filter{Limit: 50})
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		for _, ev := range feed {
			if err := enc.Encode(ev); err != nil {
				return err
			}
		}
		return nil

	case "serve":
		return serve(ctx, pool)

	default:
		return usageErr()
	}
}

// serve mounts the admin UI for local browser checks. It is DEV ONLY: the
// identity resolver authorizes every request as the same admin, so it must
// never face the public internet. Address via NOTIFY_ADDR (default :8099).
func serve(ctx context.Context, pool *pgxpool.Pool) error {
	if err := notify.Migrate(ctx, pool); err != nil {
		return err
	}
	h, err := admin.New(admin.Config{
		Store:    notify.NewStore(pool),
		SiteName: "notify (dev)",
		Resolver: admin.ResolverFunc(func(*http.Request) (notify.Identity, bool) {
			return notify.Identity{Issuer: "dev", Subject: "dev-admin", Label: "dev@local"}, true
		}),
	})
	if err != nil {
		return err
	}

	const base = "/admin/notifications"
	mux := http.NewServeMux()
	mux.Handle(base+"/", http.StripPrefix(base, h.Routes()))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, base+"/", http.StatusFound)
	})

	addr := firstNonEmpty(os.Getenv("NOTIFY_ADDR"), ":8099")
	fmt.Fprintf(os.Stderr, "notifyctl serve: DEV ONLY — auth is stubbed (every visitor is admin). listening on %s%s/\n", addr, base)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}

func usageErr() error {
	return fmt.Errorf("usage: notifyctl <migrate|emit-test|feed-dump|serve>\n" +
		"  DSN via NOTIFY_DATABASE_URL or DATABASE_URL; serve addr via NOTIFY_ADDR")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
