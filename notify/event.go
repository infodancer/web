package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Severity ranks an event for the admin feed. Empty defaults to [SeverityInfo].
type Severity string

// Severity levels, ordered from least to most urgent.
const (
	SeverityInfo   Severity = "info"
	SeverityNotice Severity = "notice"
	SeverityWarn   Severity = "warn"
	SeverityAlert  Severity = "alert"
)

func (s Severity) valid() bool {
	switch s {
	case SeverityInfo, SeverityNotice, SeverityWarn, SeverityAlert:
		return true
	}
	return false
}

// Audience routes an event. Empty defaults to [AudienceAdmin]. Phase 1 only
// produces admin events; user fan-out arrives in a later phase.
type Audience string

// Audience values route an event to admins, users, or both over time.
const (
	AudienceAdmin Audience = "admin"
	AudienceUser  Audience = "user"
)

func (a Audience) valid() bool {
	switch a {
	case AudienceAdmin, AudienceUser:
		return true
	}
	return false
}

// Identity is an opaque actor or subject reference. It is never a foreign key
// into a host table; Label is a point-in-time display cache (e.g. an email at
// event time) and must not be treated as authoritative.
type Identity struct {
	Issuer  string
	Subject string
	Label   string
}

// Event is the flexible notification object. The typed fields are what the
// admin feed filters and sorts on; Payload carries everything site-specific.
type Event struct {
	// ID is assigned by the store on insert and populated on read; it is
	// ignored by [Emitter.Emit].
	ID int64

	Type     string   // required, e.g. "user.created", "campaign.created"
	Source   string   // required, the producing site, e.g. "osg"
	Title    string   // required, human-readable one-liner for the feed
	Severity Severity // defaults to SeverityInfo
	Audience Audience // defaults to AudienceAdmin

	Actor   *Identity // who caused the event; nil means system/anonymous
	Subject *Identity // who or what the event concerns; optional

	// Payload is free-form and stored as JSONB. It must be JSON-marshalable.
	Payload map[string]any

	// CreatedAt is set by the store from the database clock on read.
	CreatedAt time.Time
}

// Validation sentinels. Wrapped with %w so callers can errors.Is them.
var (
	ErrMissingType     = errors.New("notify: event Type is required")
	ErrMissingSource   = errors.New("notify: event Source is required")
	ErrMissingTitle    = errors.New("notify: event Title is required")
	ErrInvalidSeverity = errors.New("notify: invalid event Severity")
	ErrInvalidAudience = errors.New("notify: invalid event Audience")
	ErrInvalidPayload  = errors.New("notify: event Payload is not JSON-marshalable")
)

// normalize trims the string fields, applies defaults, and validates. It is
// idempotent, so calling it more than once on the path to storage is harmless.
func (e *Event) normalize() error {
	e.Type = strings.TrimSpace(e.Type)
	e.Source = strings.TrimSpace(e.Source)
	e.Title = strings.TrimSpace(e.Title)

	switch {
	case e.Type == "":
		return ErrMissingType
	case e.Source == "":
		return ErrMissingSource
	case e.Title == "":
		return ErrMissingTitle
	}

	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	if !e.Severity.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidSeverity, e.Severity)
	}

	if e.Audience == "" {
		e.Audience = AudienceAdmin
	}
	if !e.Audience.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAudience, e.Audience)
	}
	return nil
}

// payloadJSON marshals Payload to the bytes stored in the JSONB column. A nil
// or empty payload becomes an empty JSON object so the column is never NULL.
func (e Event) payloadJSON() ([]byte, error) {
	if len(e.Payload) == 0 {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	return b, nil
}

// decodePayload unmarshals the stored JSONB column back into a map. Empty
// bytes decode to a nil map.
func decodePayload(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("notify: decode payload: %w", err)
	}
	return m, nil
}
