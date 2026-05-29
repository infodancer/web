// Package handler provides a reusable HTTP subscribe handler for newsletter sites.
package handler

import (
	"log"
	"net/http"

	"github.com/infodancer/web/newsletter/antibot"
	"github.com/infodancer/web/newsletter/listmonk"
	"github.com/infodancer/web/newsletter/subscribers"
)

// Subscribe is an http.Handler that accepts POST /subscribe requests,
// records the email in a local SQLite store, and forwards to Listmonk.
// Response messages are configurable HTML fragments.
type Subscribe struct {
	Store    *subscribers.Store
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
func New(store *subscribers.Store, lm *listmonk.Client, listUUID string) *Subscribe {
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

	// ParseMultipartForm handles both multipart (fetch/FormData) and
	// URL-encoded (HTMX default) bodies. Ignore error for non-multipart.
	r.ParseMultipartForm(1 << 20) //nolint:errcheck

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	email := r.FormValue("email")
	if email == "" {
		_, _ = w.Write([]byte(h.MsgEmpty))
		return
	}

	if err := antibot.ValidateMX(email); err != nil {
		log.Printf("MX validation failed for %s: %v", email, err)
		w.Write([]byte(h.MsgInvalidDomain)) //nolint:errcheck
		return
	}

	isNew, err := h.Store.Insert(email)
	if err != nil {
		log.Printf("db error for %s: %v", email, err)
		_, _ = w.Write([]byte(h.MsgError))
		return
	}

	if h.ListUUID != "" {
		if err := h.Listmonk.Subscribe(email, h.ListUUID); err != nil {
			log.Printf("listmonk error for %s: %v", email, err)
			// Don't fail the request — email is safely in SQLite.
		} else if err := h.Store.MarkSynced(email); err != nil {
			log.Printf("mark synced error for %s: %v", email, err)
		}
	}

	if !isNew {
		_, _ = w.Write([]byte(h.MsgDupe))
		return
	}

	_, _ = w.Write([]byte(h.MsgSuccess))
}
