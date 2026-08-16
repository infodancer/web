package claim

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/infodancer/web/claim/db"
)

// invitationFrom converts a generated row into the package's public type.
//
// The public type deliberately drops TokenHash: no caller has a use for it, and
// a struct that never carries the hash cannot leak it into a log line, a
// template, or a JSON response by accident.
func invitationFrom(r db.ClaimInvitation) *Invitation {
	return &Invitation{
		ID:           r.ID,
		EntityType:   r.EntityType,
		EntityRef:    r.EntityRef,
		InvitedEmail: r.InvitedEmail,
		CreatedBy:    r.CreatedBy,
		CreatedAt:    r.CreatedAt.Time,
		ExpiresAt:    r.ExpiresAt.Time,
		ConsumedAt:   optTime(r.ConsumedAt),
		ConsumedBy:   r.ConsumedBy,
		RevokedAt:    optTime(r.RevokedAt),
	}
}

// optTime maps a nullable timestamptz to a *time.Time, so "never consumed" is
// nil rather than a zero time that reads as 1 January year 1.
func optTime(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
