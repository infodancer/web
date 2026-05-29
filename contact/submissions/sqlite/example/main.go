// Example: minimal contact-form server backed by SQLite + SMTP.
//
//	go run ./example
//
// Then POST a form to http://localhost:8080/contact with name, email, and
// message fields. Configure SMTP via env vars (see below).
package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/infodancer/web/contact/handler"
	"github.com/infodancer/web/contact/smtp"
	"github.com/infodancer/web/contact/submissions/sqlite"
)

func main() {
	dbPath := getenv("CONTACT_DB", "data/contact.db")
	to := getenv("CONTACT_TO", "")
	from := getenv("CONTACT_FROM", "")
	if to == "" || from == "" {
		log.Fatal("CONTACT_TO and CONTACT_FROM must be set")
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		log.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close() //nolint:errcheck

	port, err := strconv.Atoi(getenv("SMTP_PORT", "587"))
	if err != nil {
		log.Fatalf("SMTP_PORT: %v", err)
	}
	if os.Getenv("SMTP_HOST") == "" {
		log.Fatal("SMTP_HOST must be set")
	}
	mailer := smtp.New(smtp.Config{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		UseTLS:   port == 465,
	})

	h := handler.New(store, mailer, to, from)

	mux := http.NewServeMux()
	mux.Handle("/contact", h)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`ok`))
	})

	addr := ":" + getenv("PORT", "8080")
	log.Printf("contact example listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

	// In a real server, signal-handle graceful shutdown here:
	//   srv.Shutdown(ctx); h.Wait()
	// to drain in-flight email sends before exiting.
	h.Wait()
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
