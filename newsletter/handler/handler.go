// Package handler provides a reusable HTTP subscribe handler for newsletter sites.
package handler

import (
	"log"
	"net/http"

	"github.com/infodancer/web/newsletter/antibot"
	"github.com/infodancer/web/newsletter/listmonk"
)

// Store records subscribers and tracks their sync state. It is the subset of
// *subscribers.Store the handler uses, declared as an interface so the handler
// pulls in no database driver and can be tested with an in-memory fake.
type Store interface {
	Insert(email string) (bool, error)
	MarkSynced(email string) error
}

// Subscribe is an http.Handler that accepts POST /subscribe requests,
// records the email in the subscriber store, and forwards to Listmonk.
// Response messages are configurable HTML fragments.
type Subscribe struct {
	Store    Store
	Listmonk *listmonk.Client
	ListUUID string

	// Response HTML fragments written back to the client.
	MsgEmpty         string
	MsgError         string
	MsgDupe          string
	MsgSuccess       string
	MsgInvalidDomain string
}

// New returns a Subscribe handler with default response messages.
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
	}
}

func (h *Subscribe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Bound the body, then parse. ParseMultipartForm handles both multipart
	// (fetch/FormData) and URL-encoded (HTMX default) bodies; the error is
	// ignored for non-multipart requests.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	r.ParseMultipartForm(1 << 20) //nolint:errcheck,gosec // body bounded above; non-multipart parse error is expected

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	email := r.FormValue("email")
	if email == "" {
		_, _ = w.Write([]byte(h.MsgEmpty))
		return
	}

	if err := antibot.ValidateMX(email); err != nil {
		log.Printf("MX validation failed for %q: %v", email, err)
		_, _ = w.Write([]byte(h.MsgInvalidDomain))
		return
	}

	isNew, err := h.Store.Insert(email)
	if err != nil {
		log.Printf("db error for %q: %v", email, err)
		_, _ = w.Write([]byte(h.MsgError))
		return
	}

	if h.ListUUID != "" {
		if err := h.Listmonk.Subscribe(email, h.ListUUID); err != nil {
			log.Printf("listmonk error for %q: %v", email, err)
			// Don't fail the request — email is safely in the database.
		} else if err := h.Store.MarkSynced(email); err != nil {
			log.Printf("mark synced error for %q: %v", email, err)
		}
	}

	if !isNew {
		_, _ = w.Write([]byte(h.MsgDupe))
		return
	}

	_, _ = w.Write([]byte(h.MsgSuccess))
}
