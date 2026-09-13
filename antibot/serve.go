package antibot

import (
	"context"
	"net/http"
	"time"
)

// TokenHandler serves a fresh timing token for form, for a page that cannot
// embed one when it is rendered -- a static site whose HTML was built long
// before the visitor arrived. The page fetches the token when it loads and
// puts it in the hidden token field; the minimum fill time then runs from the
// load, as it would for a rendered form.
//
// The response is never cached. A token records when it was issued, so a
// cached one would be handed to every later visitor already aged, and would
// expire for all of them at once.
//
// Serving a token records nothing: a visitor who reloads the page spends none
// of their budget, and a bot that fetches tokens in bulk gains nothing it
// could not get by fetching the page, since every submission is still counted.
func (g *Guard) TokenHandler(form string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte(g.Token(form)))
	})
}

// SweepEvery sweeps expired rate-limit windows once at once and then every
// interval, until ctx is done. Run it on its own goroutine:
//
//	go guard.SweepEvery(ctx, time.Hour)
//
// No caller waits for the result, so a failed sweep is logged at warn rather
// than returned. A missed sweep costs only table size -- expired windows are
// never counted -- so the loop carries on.
func (g *Guard) SweepEvery(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := g.Sweep(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			g.log.WarnContext(ctx, "antibot_sweep_failed", "err", err)
		} else if n > 0 {
			g.log.DebugContext(ctx, "antibot_swept", "windows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
