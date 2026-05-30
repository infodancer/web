// Command faq-serve runs the faq module against a local Postgres for
// visual / browser-based development. It is NOT a production server —
// the bundled dev user resolver returns the same hardcoded identity for
// every request when configured, has no rate limiting, and skips most of
// the production hardening a real host application would add.
//
// Configuration (all via environment):
//
//	FAQ_DSN         Postgres DSN (preferred). default postgres://localhost/faq?sslmode=disable
//	DATABASE_URL    Postgres DSN (fallback if FAQ_DSN unset).
//	PORT            HTTP listen port.    default 8080
//	BASE_PATH       Mount path.          default /faq
//	SITE_NAME       Brand text in nav.   default "" (chrome falls back to "FAQ")
//	HOME_URL        Brand link target.   default /
//	FAQ_DEV_AS      Dev identity.        default "" (anonymous)
//	                  - empty          → unauthenticated
//	                  - "admin"        → admin user "dev-admin"
//	                  - any other name → regular user with that DisplayName
//	AUTO_MIGRATE    Run goose on start.  default "true"
//
// Example:
//
//	DATABASE_URL=postgres://localhost/faq?sslmode=disable \
//	  SITE_NAME="speculative fiction" FAQ_DEV_AS=admin \
//	  go run ./cmd/faq-serve
//
// Then open http://localhost:8080/faq/ in a browser.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/infodancer/web/faq"
	"github.com/infodancer/web/faq/faquser"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "faq-serve: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := loadConfig()
	logger.Info("faq-serve starting",
		"port", cfg.port,
		"base_path", cfg.basePath,
		"site_name", cfg.siteName,
		"dev_as", cfg.devAs,
		"auto_migrate", cfg.autoMigrate)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.dbURL)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	module, err := faq.New(faq.Config{
		DB:          pool,
		Resolver:    devResolver(cfg.devAs),
		Logger:      logger,
		BasePath:    cfg.basePath,
		SiteName:    cfg.siteName,
		HomeURL:     cfg.homeURL,
		AutoMigrate: cfg.autoMigrate,
	})
	if err != nil {
		return fmt.Errorf("faq.New: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(cfg.basePath+"/", http.StripPrefix(cfg.basePath, module.Routes()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, cfg.basePath+"/", http.StatusFound)
	})

	server := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           requestLogger(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "url", "http://localhost:"+cfg.port+cfg.basePath+"/")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

type config struct {
	dbURL       string
	port        string
	basePath    string
	siteName    string
	homeURL     string
	devAs       string
	autoMigrate bool
}

func loadConfig() config {
	c := config{
		dbURL:       envOr("FAQ_DSN", envOr("DATABASE_URL", "postgres://localhost/faq?sslmode=disable")),
		port:        envOr("PORT", "8080"),
		basePath:    envOr("BASE_PATH", "/faq"),
		siteName:    os.Getenv("SITE_NAME"),
		homeURL:     envOr("HOME_URL", "/"),
		devAs:       os.Getenv("FAQ_DEV_AS"),
		autoMigrate: envOr("AUTO_MIGRATE", "true") == "true",
	}
	// Normalize basePath: must start with "/" and not end with "/".
	if !strings.HasPrefix(c.basePath, "/") {
		c.basePath = "/" + c.basePath
	}
	c.basePath = strings.TrimRight(c.basePath, "/")
	return c
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// devResolver returns a UserResolver that authenticates every request as
// the same hardcoded identity. Dev-only — production hosts wire a real
// OIDC adapter.
func devResolver(name string) faq.UserResolver {
	if name == "" {
		return faquser.UserResolverFunc(func(context.Context, *http.Request) (*faquser.User, error) {
			return nil, nil
		})
	}
	user := &faquser.User{
		Subject:     "dev:" + name,
		Issuer:      "faq-serve",
		DisplayName: name,
		Admin:       name == "admin",
	}
	return faquser.UserResolverFunc(func(context.Context, *http.Request) (*faquser.User, error) {
		return user, nil
	})
}

// requestLogger emits one line per request. Minimal — no tracing, no
// structured access logs.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		logger.Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"dur", time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}
