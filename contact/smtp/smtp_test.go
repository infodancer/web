package smtp

import (
	"strings"
	"testing"

	"github.com/infodancer/web/contact/handler"
)

func TestEnvelopeRecipients(t *testing.T) {
	tests := []struct {
		name string
		msg  handler.Message
		want []string
	}{
		{
			name: "to only",
			msg:  handler.Message{To: []string{"a@x"}},
			want: []string{"a@x"},
		},
		{
			name: "to cc bcc in order",
			msg: handler.Message{
				To:  []string{"a@x", "b@x"},
				Cc:  []string{"c@x"},
				Bcc: []string{"d@x", "e@x"},
			},
			want: []string{"a@x", "b@x", "c@x", "d@x", "e@x"},
		},
		{
			name: "bcc reaches the envelope",
			msg:  handler.Message{To: []string{"a@x"}, Bcc: []string{"secret@x"}},
			want: []string{"a@x", "secret@x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envelopeRecipients(tt.msg)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestBuildRFC5322_CcHeaderPresentBccAbsent(t *testing.T) {
	msg := handler.Message{
		From:    "from@x",
		To:      []string{"to@x"},
		Cc:      []string{"cc1@x", "cc2@x"},
		Bcc:     []string{"secret@x"},
		ReplyTo: "visitor@x",
		Subject: "hi",
		Body:    "hello",
	}
	out := buildRFC5322(msg)

	header, _, ok := strings.Cut(out, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in:\n%s", out)
	}

	if !strings.Contains(header, "\r\nTo: to@x\r\n") {
		t.Errorf("To header missing or wrong:\n%s", header)
	}
	if !strings.Contains(header, "\r\nCc: cc1@x, cc2@x\r\n") {
		t.Errorf("Cc header missing or wrong:\n%s", header)
	}
	// The whole point of Bcc: it must never appear in the headers.
	if strings.Contains(header, "Bcc") || strings.Contains(header, "secret@x") {
		t.Errorf("Bcc leaked into headers:\n%s", header)
	}
}

func TestBuildRFC5322_NoCcHeaderWhenEmpty(t *testing.T) {
	msg := handler.Message{
		From:    "from@x",
		To:      []string{"to@x"},
		Subject: "hi",
		Body:    "hello",
	}
	out := buildRFC5322(msg)
	if strings.Contains(out, "Cc:") {
		t.Errorf("Cc header emitted for empty Cc:\n%s", out)
	}
}

// CR/LF embedded in any header value must not be able to inject extra header
// lines. scrubHeader folds them to spaces, so the smuggled "header" stays on
// the value's own line and never becomes a line of its own.
func TestBuildRFC5322_HeaderInjectionScrubbed(t *testing.T) {
	msg := handler.Message{
		From:    "from@x\r\nX-From-Inject: yes",
		To:      []string{"to@x\r\nBcc: leak@evil.example"},
		Cc:      []string{"cc@x\r\nX-Cc-Inject: yes"},
		ReplyTo: "visitor@x\r\nX-Reply-Inject: yes",
		Subject: "hi\r\nX-Subject-Inject: yes",
		Body:    "hello",
	}
	out := buildRFC5322(msg)
	header, _, ok := strings.Cut(out, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in:\n%s", out)
	}

	// No line in the header block may be one of the injected headers.
	for _, line := range strings.Split(header, "\r\n") {
		switch {
		case strings.HasPrefix(line, "X-From-Inject"),
			strings.HasPrefix(line, "Bcc:"),
			strings.HasPrefix(line, "X-Cc-Inject"),
			strings.HasPrefix(line, "X-Reply-Inject"),
			strings.HasPrefix(line, "X-Subject-Inject"):
			t.Errorf("injected header survived as its own line: %q", line)
		}
	}
	// And no bare CR/LF should remain inside the header block at all beyond the
	// CRLF line terminators handled above.
	for _, line := range strings.Split(header, "\r\n") {
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("header line still contains a bare CR/LF: %q", line)
		}
	}
}
