package notify

import (
	"errors"
	"math"
	"testing"
)

func validEvent() Event {
	return Event{Type: "user.created", Source: "osg", Title: "New account"}
}

func TestEventNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       Event
		wantErr  error
		wantSev  Severity
		wantAud  Audience
		wantType string
	}{
		{
			name:    "defaults applied",
			in:      validEvent(),
			wantSev: SeverityInfo,
			wantAud: AudienceAdmin,
		},
		{
			name:     "trims whitespace",
			in:       Event{Type: "  user.created  ", Source: " osg ", Title: " hi "},
			wantSev:  SeverityInfo,
			wantAud:  AudienceAdmin,
			wantType: "user.created",
		},
		{
			name:    "explicit severity and audience preserved",
			in:      Event{Type: "t", Source: "s", Title: "x", Severity: SeverityAlert, Audience: AudienceUser},
			wantSev: SeverityAlert,
			wantAud: AudienceUser,
		},
		{
			name:    "missing type",
			in:      Event{Source: "osg", Title: "x"},
			wantErr: ErrMissingType,
		},
		{
			name:    "blank type after trim",
			in:      Event{Type: "   ", Source: "osg", Title: "x"},
			wantErr: ErrMissingType,
		},
		{
			name:    "missing source",
			in:      Event{Type: "t", Title: "x"},
			wantErr: ErrMissingSource,
		},
		{
			name:    "missing title",
			in:      Event{Type: "t", Source: "s"},
			wantErr: ErrMissingTitle,
		},
		{
			name:    "invalid severity",
			in:      Event{Type: "t", Source: "s", Title: "x", Severity: "loud"},
			wantErr: ErrInvalidSeverity,
		},
		{
			name:    "invalid audience",
			in:      Event{Type: "t", Source: "s", Title: "x", Audience: "everyone"},
			wantErr: ErrInvalidAudience,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ev := tt.in
			err := ev.normalize()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("normalize() err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize() unexpected err = %v", err)
			}
			if ev.Severity != tt.wantSev {
				t.Errorf("severity = %q, want %q", ev.Severity, tt.wantSev)
			}
			if ev.Audience != tt.wantAud {
				t.Errorf("audience = %q, want %q", ev.Audience, tt.wantAud)
			}
			if tt.wantType != "" && ev.Type != tt.wantType {
				t.Errorf("type = %q, want %q", ev.Type, tt.wantType)
			}
		})
	}
}

func TestEventPayloadJSON(t *testing.T) {
	t.Parallel()

	t.Run("nil payload is empty object", func(t *testing.T) {
		t.Parallel()
		ev := validEvent()
		b, err := ev.payloadJSON()
		if err != nil {
			t.Fatalf("payloadJSON() err = %v", err)
		}
		if string(b) != "{}" {
			t.Errorf("payloadJSON() = %q, want {}", b)
		}
	})

	t.Run("round-trips through decode", func(t *testing.T) {
		t.Parallel()
		ev := validEvent()
		ev.Payload = map[string]any{"campaign": "Dragons", "count": float64(3)}
		b, err := ev.payloadJSON()
		if err != nil {
			t.Fatalf("payloadJSON() err = %v", err)
		}
		got, err := decodePayload(b)
		if err != nil {
			t.Fatalf("decodePayload() err = %v", err)
		}
		if got["campaign"] != "Dragons" || got["count"] != float64(3) {
			t.Errorf("round-trip = %#v", got)
		}
	})

	t.Run("unmarshalable payload errors", func(t *testing.T) {
		t.Parallel()
		ev := validEvent()
		ev.Payload = map[string]any{"bad": math.Inf(1)} // +Inf is not valid JSON
		if _, err := ev.payloadJSON(); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("payloadJSON() err = %v, want ErrInvalidPayload", err)
		}
	})
}

func TestDecodePayloadEmpty(t *testing.T) {
	t.Parallel()
	got, err := decodePayload(nil)
	if err != nil {
		t.Fatalf("decodePayload(nil) err = %v", err)
	}
	if got != nil {
		t.Errorf("decodePayload(nil) = %#v, want nil", got)
	}
}
