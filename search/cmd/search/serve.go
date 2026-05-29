package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/search"
)

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "path to TOML config")
		dsn        = fs.String("db", "", "Postgres DSN (overrides config/env)")
		addr       = fs.String("addr", "", "listen address (default :8080)")
		basePath   = fs.String("base-path", "", "URL mount prefix (default /search)")
		siteName   = fs.String("site-name", "", "chrome brand name")
		migrate    = fs.Bool("migrate", false, "run migrations on startup (needs RW role)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	fc, err := loadFileConfig(*configPath)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db := firstNonEmpty(*dsn, fc.DB.QueryDSN, os.Getenv("DATABASE_URL"))
	if db == "" {
		return fmt.Errorf("no database DSN (set --db, db.query_dsn, or DATABASE_URL)")
	}
	listen := firstNonEmpty(*addr, fc.Serve.Addr, ":8080")
	mount := firstNonEmpty(*basePath, fc.Serve.BasePath, "/search")

	// Optional embedder (vector arm) and reranker, each opt-in via env. With
	// neither set the corpus is served FTS-only.
	embedder, err := buildEmbedder()
	if err != nil {
		return err
	}
	reranker, err := buildReranker()
	if err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, db)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	module, err := search.New(search.Config{
		DB:          pool,
		Logger:      logger,
		BasePath:    mount,
		SiteName:    firstNonEmpty(*siteName, fc.Serve.SiteName),
		HomeURL:     fc.Serve.HomeURL,
		Embedder:    embedder,
		Reranker:    reranker,
		EmbedMetric: os.Getenv("SEARCH_EMBED_METRIC"),
		AutoMigrate: *migrate,
	})
	if err != nil {
		return fmt.Errorf("search.New: %w", err)
	}
	logger.Info("query path", "vector_arm", embedder != nil, "reranker", reranker != nil)

	mux := http.NewServeMux()
	mux.Handle(mount+"/", http.StripPrefix(mount, module.Routes()))
	// Convenience redirect from the bare mount path to its trailing-slash form.
	mux.HandleFunc(mount, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mount+"/", http.StatusMovedPermanently)
	})

	srv := &http.Server{
		Addr:              listen,
		Handler:           requestLogger(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	logger.Info("serving", "addr", listen, "mount", mount+"/")

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// requestLogger logs each request's method, path, and duration at debug-ish
// info level. Minimal; the host wires its own access log in production.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}
