package httpsec

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

// ServerConfig tunes [NewServer]. Zero fields take the defaults.
type ServerConfig struct {
	// ReadHeaderTimeout bounds reading request headers (default 10s). It is
	// the Slowloris defense; Go's http.Server has none unless it is set.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds reading the whole request (default 15s).
	ReadTimeout time.Duration
	// WriteTimeout bounds writing the response (default 30s).
	WriteTimeout time.Duration
	// IdleTimeout bounds an idle keep-alive connection (default 120s).
	IdleTimeout time.Duration
	// MaxHeaderBytes bounds request headers (default 256 KiB).
	MaxHeaderBytes int
	// ErrorLog receives server errors; route it into slog. Nil uses the
	// standard logger.
	ErrorLog *log.Logger
}

// NewServer returns an *http.Server with every limit set explicitly.
func NewServer(addr string, h http.Handler, cfg ServerConfig) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: durOr(cfg.ReadHeaderTimeout, 10*time.Second),
		ReadTimeout:       durOr(cfg.ReadTimeout, 15*time.Second),
		WriteTimeout:      durOr(cfg.WriteTimeout, 30*time.Second),
		IdleTimeout:       durOr(cfg.IdleTimeout, 120*time.Second),
		MaxHeaderBytes:    intOr(cfg.MaxHeaderBytes, 256<<10),
		ErrorLog:          cfg.ErrorLog,
	}
}

func durOr(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

func intOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// Serve runs srv on ln until ctx is cancelled, then shuts down gracefully:
// it stops accepting, lets in-flight requests finish for up to grace, and
// closes whatever is left. It returns nil after a clean shutdown, the serve
// error if the listener fails, and an error wrapping context.DeadlineExceeded
// if requests were still running when the grace period ended. Pass a context
// from signal.NotifyContext to shut down on SIGINT and SIGTERM.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		return errors.Join(fmt.Errorf("httpsec: graceful shutdown: %w", err), serveResult(errc))
	}
	return serveResult(errc)
}

// serveResult collects what srv.Serve returned once the server has been shut
// down or closed. http.ErrServerClosed is the expected outcome, not an error.
func serveResult(errc <-chan error) error {
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
