// Package timelineuser holds the authentication value types the host
// application uses to drive the timeline module. They live in a separate
// package so the module's internal handlers can import them without creating an
// import cycle with the top-level [timeline] package, which re-exports the same
// names via type aliases.
package timelineuser

import (
	"context"
	"net/http"
)

// User describes the authenticated subject for a request. The module never runs
// an OIDC flow itself — the host application supplies a [UserResolver] that
// produces this from whatever session mechanism it already runs.
type User struct {
	// Subject is the OIDC `sub` claim. Combined with Issuer to form the stable
	// identity hash stored in timeline_users.
	Subject string

	// Issuer is the OIDC `iss` claim.
	Issuer string

	// DisplayName is the human-readable name for the user.
	DisplayName string

	// Email is optional; present when the upstream identity supplies it.
	Email string

	// Admin grants management privileges: editing timelines, importing, syncing,
	// and viewing non-public timelines. Read access to public timelines needs no
	// user at all.
	Admin bool
}

// UserResolver extracts a [*User] from an incoming request. Returning
// (nil, nil) means "anonymous" — read-only access to public timelines.
// Returning (nil, err) indicates a present-but-invalid token and produces a 401
// response.
type UserResolver interface {
	Resolve(ctx context.Context, r *http.Request) (*User, error)
}

// UserResolverFunc adapts a function into a [UserResolver].
type UserResolverFunc func(ctx context.Context, r *http.Request) (*User, error)

// Resolve calls f(ctx, r).
func (f UserResolverFunc) Resolve(ctx context.Context, r *http.Request) (*User, error) {
	return f(ctx, r)
}

type userContextKey struct{}

// ContextWithUser returns a new context carrying u. Handlers retrieve the user
// via [UserFromContext].
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFromContext returns the user previously stored in ctx by
// [ContextWithUser], or nil if no user is present.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey{}).(*User)
	return u
}
