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
