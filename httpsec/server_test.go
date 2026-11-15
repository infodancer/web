package httpsec_test

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/infodancer/web/httpsec"
)

func TestNewServerDefaults(t *testing.T) {
	h := okHandler()
	srv := httpsec.NewServer(":0", h, httpsec.ServerConfig{})
	if srv.Addr != ":0" || srv.Handler == nil {
		t.Errorf("addr/handler not set: %q %v", srv.Addr, srv.Handler)
	}
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout,
		"ReadTimeout":       srv.ReadTimeout,
		"WriteTimeout":      srv.WriteTimeout,
		"IdleTimeout":       srv.IdleTimeout,
	} {
		if d <= 0 {
			t.Errorf("%s = %v, want a positive default", name, d)
		}
	}
	if srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Errorf("ReadHeaderTimeout %v exceeds ReadTimeout %v", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	if srv.MaxHeaderBytes <= 0 {
		t.Errorf("MaxHeaderBytes = %d, want an explicit limit", srv.MaxHeaderBytes)
	}
}

func TestNewServerOverrides(t *testing.T) {
	lg := log.New(io.Discard, "", 0)
	srv := httpsec.NewServer(":0", okHandler(), httpsec.ServerConfig{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		MaxHeaderBytes:    4096,
		ErrorLog:          lg,
	})
	if srv.ReadHeaderTimeout != time.Second || srv.ReadTimeout != 2*time.Second ||
		srv.WriteTimeout != 3*time.Second || srv.IdleTimeout != 4*time.Second ||
		srv.MaxHeaderBytes != 4096 || srv.ErrorLog != lg {
		t.Errorf("overrides not applied: %+v", srv)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestServeShutsDownGracefully(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httpsec.NewServer("", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("finished"))
	}), httpsec.ServerConfig{})
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpsec.Serve(ctx, srv, ln, 5*time.Second) }()

	type result struct {
		body string
		err  error
	}
	resc := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			resc <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		resc <- result{string(b), err}
	}()

	<-started
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Serve returned (%v) while a request was still in flight", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if r := <-resc; r.err != nil || r.body != "finished" {
		t.Fatalf("in-flight request: body=%q err=%v", r.body, r.err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after the request finished")
	}
}

func TestServeForcesCloseAfterGrace(t *testing.T) {
	started := make(chan struct{})
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	srv := httpsec.NewServer("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-hold
	}), httpsec.ServerConfig{})
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- httpsec.Serve(ctx, srv, ln, 100*time.Millisecond) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Serve = %v, want context.DeadlineExceeded when the grace period runs out", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve hung past its grace period")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	ln := listen(t)
	_ = ln.Close()
	srv := httpsec.NewServer("", okHandler(), httpsec.ServerConfig{})
	err := httpsec.Serve(context.Background(), srv, ln, time.Second)
	if err == nil {
		t.Error("Serve on a closed listener returned nil")
	}
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }
