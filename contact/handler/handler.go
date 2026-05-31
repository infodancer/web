// Package handler exposes a reusable HTTP handler that accepts contact-form
// POST submissions, persists them, and emails the configured destination.
package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/infodancer/web/contact/submissions"
)

// Mailer sends a single transactional email. Implementations live in the
// smtp/ subpackage; consumers may inject any Mailer (Postmark, SES, etc.)
// by satisfying this one-method interface.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Store persists a submission and tracks its delivery status. It is the subset
// of *submissions.Store that the handler actually uses, declared as an
// interface here so the handler does not pull in any database driver: the
// concrete store and its connection are supplied by the consumer, and tests
// can substitute an in-memory fake.
type Store interface {
	Save(ctx context.Context, sub *submissions.Submission) error
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
}

// Message is the wire representation of an outgoing email.
type Message struct {
	From    string
	To      []string
	Cc      []string // visible carbon-copy recipients; appear in the Cc: header
	Bcc     []string // blind copies; receive the mail but never appear in headers
	ReplyTo string
	Subject string
	Body    string
}

// Contact is the HTTP handler. Construct via New for sane defaults; override
// individual fields after construction for customization.
type Contact struct {
	// Required dependencies.
	Store  Store
	Mailer Mailer

	// Required addresses.
	To   []string // destination address(es); all appear in the To: header
	From string   // From: header on outgoing mail; should align with SPF/DKIM

	// Optional operator-configured copy recipients on the primary email. These
	// are set by the site operator, never read from the submitted form, so the
	// form cannot be used to mail arbitrary third parties. Cc addresses appear
	// in the Cc: header; Bcc addresses receive the mail but never appear in any
	// header. Confirmation emails to the visitor are not copied.
	Cc  []string
	Bcc []string

	// Auto-reply / confirmation back to the visitor.
	SendConfirmation     bool
	ConfirmationFrom     string             // optional override; defaults to From
	ConfirmationSubject  string             // text/template; receives Submission
	ConfirmationBodyTmpl *template.Template // text/template; receives Submission

	// Outgoing message rendering. nil → built-in defaults.
	SubjectTmpl *template.Template
	BodyTmpl    *template.Template

	// Spam protection. Honeypot field name is read from the form; if non-empty
	// the request is silently accepted (200) without persisting or emailing.
	HoneypotField string

	// Body size caps applied after trimming. Zero values use sensible defaults.
	MaxNameLen    int
	MaxSubjectLen int
	MaxMessageLen int

	// Timeout for the synchronous Save and the async email send.
	SendTimeout time.Duration

	// HTML response fragments returned to the client. Match the newsletter
	// shortcode pattern so both forms can share styling hooks.
	MsgSuccess string
	MsgInvalid string
	MsgError   string

	// Logger for diagnostic output. nil → slog.Default().
	Logger *slog.Logger

	// inFlight tracks active deliver() goroutines for graceful shutdown.
	inFlight sync.WaitGroup
}

// Wait blocks until all in-flight email deliveries complete. Call during
// graceful shutdown after the HTTP server has stopped accepting requests
// to avoid abandoning sends mid-flight.
func (h *Contact) Wait() { h.inFlight.Wait() }

// New returns a Contact handler with reasonable defaults.
//
// `to` may be a single address or a comma-separated list ("a@x,b@y"); all
// addresses end up in the To: header. Whitespace and empty entries are
// trimmed, so values pulled from env vars work directly.
func New(store Store, mailer Mailer, to, from string) *Contact {
	c := &Contact{
		Store:         store,
		Mailer:        mailer,
		To:            ParseAddrList(to),
		From:          from,
		HoneypotField: "website",
		MaxNameLen:    200,
		MaxSubjectLen: 200,
		MaxMessageLen: 10_000,
		SendTimeout:   15 * time.Second,
		MsgSuccess:    `<p class="contact-msg contact-msg--ok">Thanks &mdash; I&rsquo;ll be in touch.</p>`,
		MsgInvalid:    `<p class="contact-msg contact-msg--err">Please fill in name, email, and a message.</p>`,
		MsgError:      `<p class="contact-msg contact-msg--err">Something went wrong. Please try again.</p>`,
	}
	c.SubjectTmpl = template.Must(template.New("subject").Parse(defaultSubjectTmpl))
	c.BodyTmpl = template.Must(template.New("body").Parse(defaultBodyTmpl))
	return c
}

const defaultSubjectTmpl = `[contact] {{ if .Subject }}{{ .Subject }}{{ else }}(no subject){{ end }}`

const defaultBodyTmpl = `From: {{ .Name }} <{{ .Email }}>
{{ if .Subject }}Subject: {{ .Subject }}
{{ end }}
{{ .Message }}

---
Submitted at: {{ .CreatedAt.UTC.Format "2006-01-02 15:04:05 UTC" }}
{{- if .IP }}
IP: {{ .IP }}
{{- end }}
`

// ServeHTTP handles POST /contact. Other methods get 405.
func (h *Contact) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger := h.logger()

	// Accept both URL-encoded and multipart bodies (matches the newsletter pattern).
	r.ParseMultipartForm(1 << 20) //nolint:errcheck

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Honeypot: a real user never fills this hidden field. If we see content
	// here, return success without doing anything — never tell the bot it lost.
	if h.HoneypotField != "" && r.FormValue(h.HoneypotField) != "" {
		logger.Info("contact: honeypot triggered",
			"ip", clientIP(r),
			"field", h.HoneypotField)
		_, _ = w.Write([]byte(h.MsgSuccess))
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	subject := strings.TrimSpace(r.FormValue("subject"))
	message := strings.TrimSpace(r.FormValue("message"))

	if name == "" || email == "" || message == "" {
		_, _ = w.Write([]byte(h.MsgInvalid))
		return
	}

	if _, err := mail.ParseAddress(email); err != nil {
		_, _ = w.Write([]byte(h.MsgInvalid))
		return
	}

	if len(name) > h.MaxNameLen ||
		len(subject) > h.MaxSubjectLen ||
		len(message) > h.MaxMessageLen {
		_, _ = w.Write([]byte(h.MsgInvalid))
		return
	}

	// Strip CR/LF from name and subject — these can leak into the email
	// headers and enable header injection if the templates are tweaked.
	name = stripCRLF(name)
	subject = stripCRLF(subject)

	sub := &submissions.Submission{
		Name:      name,
		Email:     email,
		Subject:   subject,
		Message:   message,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}

	saveCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.Store.Save(saveCtx, sub); err != nil {
		logger.Error("contact: store save failed", "err", err, "ip", sub.IP)
		_, _ = w.Write([]byte(h.MsgError))
		return
	}

	// Persistence succeeded — tell the user immediately, send email async.
	h.inFlight.Add(1)
	go h.deliver(*sub)

	_, _ = w.Write([]byte(h.MsgSuccess))
}

// deliver sends the primary email and (optionally) a confirmation, then
// updates the submission's status row. Runs in its own goroutine.
func (h *Contact) deliver(sub submissions.Submission) {
	defer h.inFlight.Done()
	logger := h.logger().With("submission_id", sub.ID)

	ctx, cancel := context.WithTimeout(context.Background(), h.SendTimeout)
	defer cancel()

	primary, err := h.renderPrimary(sub)
	if err != nil {
		logger.Error("contact: render primary failed", "err", err)
		h.markFailed(ctx, sub.ID, "render: "+err.Error())
		return
	}

	if err := h.Mailer.Send(ctx, primary); err != nil {
		logger.Error("contact: send primary failed", "err", err)
		h.markFailed(ctx, sub.ID, err.Error())
		return
	}
	logger.Info("contact: primary delivered", "to", primary.To, "from", primary.From)

	if h.SendConfirmation {
		conf, err := h.renderConfirmation(sub)
		if err != nil {
			// Primary was delivered — we mark sent regardless. Log the failure
			// so it doesn't disappear silently.
			logger.Warn("contact: confirmation render failed", "err", err)
		} else if err := h.Mailer.Send(ctx, conf); err != nil {
			logger.Warn("contact: confirmation send failed", "err", err)
		} else {
			logger.Info("contact: confirmation delivered", "to", conf.To)
		}
	}

	if err := h.Store.MarkSent(ctx, sub.ID); err != nil {
		logger.Error("contact: mark sent failed", "err", err)
	}
}

func (h *Contact) renderPrimary(sub submissions.Submission) (Message, error) {
	subjectBuf := &bytes.Buffer{}
	if err := h.SubjectTmpl.Execute(subjectBuf, sub); err != nil {
		return Message{}, fmt.Errorf("subject: %w", err)
	}
	bodyBuf := &bytes.Buffer{}
	if err := h.BodyTmpl.Execute(bodyBuf, sub); err != nil {
		return Message{}, fmt.Errorf("body: %w", err)
	}
	return Message{
		From:    h.From,
		To:      append([]string(nil), h.To...),
		Cc:      append([]string(nil), h.Cc...),
		Bcc:     append([]string(nil), h.Bcc...),
		ReplyTo: sub.Email,
		Subject: strings.TrimSpace(subjectBuf.String()),
		Body:    bodyBuf.String(),
	}, nil
}

func (h *Contact) renderConfirmation(sub submissions.Submission) (Message, error) {
	if h.ConfirmationBodyTmpl == nil {
		return Message{}, errors.New("ConfirmationBodyTmpl not set")
	}
	subj := h.ConfirmationSubject
	if subj == "" {
		subj = "Thanks for getting in touch"
	}
	bodyBuf := &bytes.Buffer{}
	if err := h.ConfirmationBodyTmpl.Execute(bodyBuf, sub); err != nil {
		return Message{}, fmt.Errorf("confirmation body: %w", err)
	}
	from := h.ConfirmationFrom
	if from == "" {
		from = h.From
	}
	return Message{
		From:    from,
		To:      []string{sub.Email},
		Subject: subj,
		Body:    bodyBuf.String(),
	}, nil
}

func (h *Contact) markFailed(ctx context.Context, id int64, msg string) {
	if err := h.Store.MarkFailed(ctx, id, msg); err != nil {
		h.logger().Error("contact: mark failed update failed", "err", err, "id", id)
	}
}

func (h *Contact) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func stripCRLF(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// ParseAddrList splits a comma-separated address list, trimming whitespace
// and dropping empty entries. Returns nil for an empty input.
func ParseAddrList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// clientIP extracts the client IP from X-Forwarded-For (first entry),
// falling back to RemoteAddr. Appropriate when behind a trusted reverse
// proxy. Mirror of newsletter/antibot.clientIP — duplicated here because
// that helper is unexported. If antibot is ever extracted to its own
// module with an exported ClientIP, swap to that and delete this.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
