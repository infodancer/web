package handler_test

import (
	"net/url"
	"testing"
	"text/template"
)

func validForm() url.Values {
	return url.Values{
		"name":    {"Alice"},
		"email":   {"alice@example.com"},
		"subject": {"Hello"},
		"message": {"Hi there"},
	}
}

func TestCcBccOnPrimary(t *testing.T) {
	h, mailer, _ := newHandler(t)
	h.Cc = []string{"cc@example.org"}
	h.Bcc = []string{"bcc@example.org"}

	if post(h, validForm()).Code != 200 {
		t.Fatal("expected 200")
	}
	h.Wait()

	msgs := mailer.all()
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d want 1", len(msgs))
	}
	primary := msgs[0]
	if len(primary.Cc) != 1 || primary.Cc[0] != "cc@example.org" {
		t.Errorf("primary Cc = %v", primary.Cc)
	}
	if len(primary.Bcc) != 1 || primary.Bcc[0] != "bcc@example.org" {
		t.Errorf("primary Bcc = %v", primary.Bcc)
	}
}

// The rendered message must not alias the handler's slices: mutating the
// returned message must not corrupt the handler's configured recipients.
func TestCcBccNotAliased(t *testing.T) {
	h, mailer, _ := newHandler(t)
	h.Cc = []string{"cc@example.org"}
	h.Bcc = []string{"bcc@example.org"}

	if post(h, validForm()).Code != 200 {
		t.Fatal("expected 200")
	}
	h.Wait()

	mailer.all()[0].Cc[0] = "tampered@evil.example"
	if h.Cc[0] != "cc@example.org" {
		t.Errorf("handler Cc was aliased and mutated: %v", h.Cc)
	}
}

// Confirmation emails to the visitor must never carry the operator's Cc/Bcc.
func TestConfirmationHasNoCcBcc(t *testing.T) {
	h, mailer, _ := newHandler(t)
	h.Cc = []string{"cc@example.org"}
	h.Bcc = []string{"bcc@example.org"}
	h.SendConfirmation = true
	h.ConfirmationSubject = "Thanks!"
	h.ConfirmationBodyTmpl = template.Must(template.New("c").Parse("Hi {{.Name}}"))

	if post(h, validForm()).Code != 200 {
		t.Fatal("expected 200")
	}
	h.Wait()

	msgs := mailer.all()
	if len(msgs) != 2 {
		t.Fatalf("messages: got %d want 2 (primary + confirmation)", len(msgs))
	}
	conf := msgs[1]
	if conf.To[0] != "alice@example.com" {
		t.Fatalf("second message is not the confirmation: To=%v", conf.To)
	}
	if len(conf.Cc) != 0 || len(conf.Bcc) != 0 {
		t.Errorf("confirmation leaked Cc=%v Bcc=%v", conf.Cc, conf.Bcc)
	}
}
