// Package handler provides a reusable HTTP subscribe handler for newsletter sites.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/infodancer/web/antibot"
	mx "github.com/infodancer/web/newsletter/antibot"
	"github.com/infodancer/web/newsletter/listmonk"
)

// Store records subscribers and tracks their sync state. It is the subset of
// *subscribers.Store the handler uses, declared as an interface so the handler
// pulls in no database driver and can be tested with an in-memory fake.
type Store interface {
	Insert(email string) (bool, error)
	MarkSynced(email string) error
}

// Guard decides whether a submission may proceed. *antibot.Guard satisfies it;
// tests substitute a fake.
type Guard interface {
	Check(ctx context.Context, r *http.Request, form string, opts ...antibot.CheckOption) (antibot.Verdict, error)
}

// Subscribe is an http.Handler that accepts POST /subscribe requests,
// records the email in the subscriber store, and forwards to Listmonk.
// Response messages are configurable HTML fragments.
type Subscribe struct {
	Store    Store
	Listmonk *listmonk.Client
	ListUUID string

	// Guard checks each signup -- honeypot, timing token, rate limits, and the
	// address's mail domain -- before it is stored. Nil falls back to the mail
	// domain check alone, which puts no limit on volume.
	Guard Guard

	// Form names this form to the Guard, which keeps a separate token and
	// budget per form. Default "newsletter".
	Form string

	// Response HTML fragments written back to the client.
	MsgEmpty         string
	MsgError         string
	MsgDupe          string
	MsgSuccess       string
	MsgInvalidDomain string
	MsgRejected      string // the Guard refused the signup

	// Logger receives structured logs at the HTTP boundary. Defaults to
	// [slog.Default] when nil (resolved once in New). The store and Listmonk
	// client stay silent and return wrapped errors; this handler logs each
	// failure once, here, then writes a fixed HTML fragment to the client.
	Logger *slog.Logger

	// RequestID extracts a correlation id from the request context for log
	// lines, so a subscribe failure ties to the same request across components.
	// A host injects it; a nil extractor logs an empty id. The handler never
	// requires a specific middleware.
	RequestID func(ctx context.Context) string
}

// New returns a Subscribe handler with default response messages. The logger
// and request-id extractor are resolved here: a nil Logger falls back to
// [slog.Default], and a nil RequestID becomes a no-op returning "".
func New(store Store, lm *listmonk.Client, listUUID string) *Subscribe {
	return &Subscribe{
		Store:            store,
		Listmonk:         lm,
		ListUUID:         listUUID,
		MsgEmpty:         `<p class="error">Please enter a valid email address.</p>`,
		MsgError:         `<p class="error">Something went wrong. Please try again.</p>`,
		MsgDupe:          `<p class="error">You're already subscribed.</p>`,
		MsgSuccess:       `<p class="success">You're on the list. We'll be in touch.</p>`,
		MsgInvalidDomain: `<p class="error">Please check your email address and try again.</p>`,
		MsgRejected:      `<p class="error">That did not go through. Please reload the page and try again.</p>`,
		Form:             "newsletter",
		Logger:           slog.Default(),
		RequestID:        func(context.Context) string { return "" },
	}
}

// logger returns a request-scoped logger so every line for one request shares a
// request id. It is nil-safe: a handler built without New (or with the fields
// left zero) still logs through slog.Default and an empty id.
func (h *Subscribe) logger(ctx context.Context) *slog.Logger {
	log := h.Logger
	if log == nil {
		log = slog.Default()
	}
	reqID := ""
	if h.RequestID != nil {
		reqID = h.RequestID(ctx)
	}
	return log.With("request_id", reqID)
}

// writeMessage writes a fixed HTML response fragment. The Msg* fields are set
// at construction from trusted operator config, never from request input, so
// they are safe to write to a text/html response (the submitted email is never
// echoed back). Centralized so the one gosec G705 taint suppression lives here.
func writeMessage(w http.ResponseWriter, msg string) {
	_, _ = w.Write([]byte(msg)) //nolint:gosec // G705: static operator-configured message constant, not request-derived
}

// admit asks the Guard about the signup and, when the answer is no, writes this
// form's response. It reports whether the handler should carry on.
//
// The Guard fails closed: an error means its store could not be reached, and
// the signup is refused rather than let through unchecked. Rejections are
// logged by the Guard, so only the error is logged here.
func (h *Subscribe) admit(w http.ResponseWriter, r *http.Request, log *slog.Logger, email string) bool {
	form := h.Form
	if form == "" {
		form = "newsletter"
	}
	v, err := h.Guard.Check(r.Context(), r, form, antibot.Email(email))
	switch {
	case err != nil:
		log.Error("antibot check failed", "err", err)
		writeMessage(w, h.MsgError)
	case v.Allowed:
		return true
	case v.Silent:
		writeMessage(w, h.MsgSuccess)
	case v.Reason == antibot.ReasonInvalidEmail || v.Reason == antibot.ReasonNoMX:
		writeMessage(w, h.MsgInvalidDomain)
	default:
		writeMessage(w, h.MsgRejected)
	}
	return false
}

func (h *Subscribe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log := h.logger(r.Context())

	// Bound the body, then parse. ParseMultipartForm handles both multipart
	// (fetch/FormData) and URL-encoded (HTMX default) bodies; the error is
	// ignored for non-multipart requests.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	r.ParseMultipartForm(1 << 20) //nolint:errcheck,gosec // body bounded above; non-multipart parse error is expected

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	email := r.FormValue("email")
	if email == "" {
		writeMessage(w, h.MsgEmpty)
		return
	}

	if h.Guard != nil {
		if !h.admit(w, r, log, email) {
			return
		}
	} else if err := mx.ValidateMX(email); err != nil {
		// User input rejection, not a server fault: the submitted address has no
		// usable mail domain. INFO so an operator can spot abuse patterns without
		// it polluting the error stream.
		log.Info("mx validation rejected", "email", email, "err", err)
		writeMessage(w, h.MsgInvalidDomain)
		return
	}

	isNew, err := h.Store.Insert(email)
	if err != nil {
		log.Error("insert subscriber", "email", email, "err", err)
		writeMessage(w, h.MsgError)
		return
	}

	if h.ListUUID != "" {
		if err := h.Listmonk.Subscribe(email, h.ListUUID); err != nil {
			// Best-effort sync only: the email is already committed to our
			// database, so a Listmonk outage must not fail the subscribe. WARN
			// so an operator notices a persistent sync failure -- the row stays
			// unsynced and can be reconciled later -- without it counting as a
			// request failure.
			log.Warn("listmonk sync failed; subscriber saved locally", "email", email, "err", err)
		} else if err := h.Store.MarkSynced(email); err != nil {
			log.Error("mark subscriber synced", "email", email, "err", err)
		}
	}

	if !isNew {
		writeMessage(w, h.MsgDupe)
		return
	}

	writeMessage(w, h.MsgSuccess)
}
