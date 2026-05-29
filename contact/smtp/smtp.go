// Package smtp implements handler.Mailer over plain SMTP with optional
// STARTTLS and PLAIN auth — adequate for transactional volume against a
// hosted relay (Fastmail, Gmail app password, Postmark SMTP endpoint, etc.).
//
// For higher-volume or vendor-API delivery (Postmark HTTP, SES, Mailgun),
// implement handler.Mailer directly using the vendor SDK; this package is
// only one option among many.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netsmtp "net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/infodancer/web/contact/handler"
)

// Config configures a Client.
type Config struct {
	Host     string // SMTP server hostname
	Port     int    // typically 587 (STARTTLS) or 465 (implicit TLS) or 25
	Username string // PLAIN auth username (often the From address)
	Password string // PLAIN auth password / app password
	// UseTLS forces implicit TLS on connect (port 465-style). When false,
	// the client uses STARTTLS opportunistically and refuses unencrypted
	// auth.
	UseTLS bool
	// Timeout for the whole exchange. Zero → 30s.
	Timeout time.Duration
}

// Client implements handler.Mailer.
type Client struct {
	cfg Config
}

// New returns a Client with the given config. No connection is made until Send.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{cfg: cfg}
}

// Send delivers msg via SMTP. It honors ctx cancellation only at the dial
// boundary; once the SMTP exchange begins, the configured Timeout governs.
func (c *Client) Send(ctx context.Context, msg handler.Message) error {
	if c.cfg.Host == "" {
		return errors.New("smtp: empty Host")
	}
	if c.cfg.Port == 0 {
		return errors.New("smtp: zero Port")
	}
	if msg.From == "" || len(msg.To) == 0 {
		return errors.New("smtp: missing From or To")
	}

	addr := net.JoinHostPort(c.cfg.Host, strconv.Itoa(c.cfg.Port))

	dialer := &net.Dialer{Timeout: c.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(c.cfg.Timeout))

	if c.cfg.UseTLS {
		conn = tls.Client(conn, &tls.Config{ServerName: c.cfg.Host, MinVersion: tls.VersionTLS12})
		if err := conn.(*tls.Conn).HandshakeContext(ctx); err != nil {
			return fmt.Errorf("tls handshake: %w", err)
		}
	}

	client, err := netsmtp.NewClient(conn, c.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	if !c.cfg.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: c.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else {
			return errors.New("smtp: server does not support STARTTLS and UseTLS is false")
		}
	}

	if c.cfg.Username != "" {
		auth := netsmtp.PlainAuth("", c.cfg.Username, c.cfg.Password, c.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	if err := client.Mail(msg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range msg.To {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := wc.Write([]byte(buildRFC5322(msg))); err != nil {
		_ = wc.Close()
		return fmt.Errorf("write data: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return client.Quit()
}

// buildRFC5322 produces a minimal RFC-5322 message. Body is plain text;
// CRLFs are normalized; sender-controlled fields are scrubbed of bare LFs
// to avoid header injection.
func buildRFC5322(m handler.Message) string {
	var b strings.Builder
	b.Grow(len(m.Body) + 256)

	headers := [][2]string{
		{"From", scrubHeader(m.From)},
		{"To", scrubHeader(strings.Join(m.To, ", "))},
	}
	if m.ReplyTo != "" {
		headers = append(headers, [2]string{"Reply-To", scrubHeader(m.ReplyTo)})
	}
	headers = append(headers,
		[2]string{"Subject", scrubHeader(m.Subject)},
		[2]string{"Date", time.Now().UTC().Format(time.RFC1123Z)},
		[2]string{"MIME-Version", "1.0"},
		[2]string{"Content-Type", "text/plain; charset=utf-8"},
	)

	for _, h := range headers {
		b.WriteString(h[0])
		b.WriteString(": ")
		b.WriteString(h[1])
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")

	// Normalize line endings in the body to CRLF and dot-stuff lines that
	// would otherwise terminate the DATA section.
	for _, line := range strings.Split(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, ".") {
			b.WriteString(".")
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	return b.String()
}

func scrubHeader(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
