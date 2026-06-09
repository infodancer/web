// Package profileuser holds the authentication value types the host
// application uses to drive the profile module. They live in a separate
// package so the module's internal handlers can import them without creating
// an import cycle with the top-level [profile] package, which re-exports the
// same names via type aliases.
package profileuser

import (
	"context"
	"net/http"
)

// User describes the authenticated subject for a request. The module never
// runs a login flow itself -- the host application supplies a [Resolver] that
// produces this from whatever session mechanism it already runs.
type User struct {
	// Key is the host's stable, opaque identifier for the user (e.g. its
	// users.id rendered as text, or an issuer+subject pair). It is the primary
	// key profiles are stored under and must never change for a given user.
	Key string

	// DisplayName seeds the profile's display name before the user saves one
	// of their own. Typically the IdP's display-name claim.
	DisplayName string

	// Admin grants management privileges: changing another user's handle and
	// editing profiles other than one's own.
	Admin bool
}

// Resolver extracts a [*User] from an incoming request. Returning (nil, nil)
// means "anonymous" -- read-only access to public profiles. Returning
// (nil, err) is treated as anonymous; write routes enforce identity
// separately.
type Resolver interface {
	Resolve(ctx context.Context, r *http.Request) (*User, error)
}

// ResolverFunc adapts a function into a [Resolver].
type ResolverFunc func(ctx context.Context, r *http.Request) (*User, error)

// Resolve calls f(ctx, r).
func (f ResolverFunc) Resolve(ctx context.Context, r *http.Request) (*User, error) {
	return f(ctx, r)
}

type userContextKey struct{}

// ContextWithUser returns a new context carrying u. Handlers retrieve the
// user via [UserFromContext].
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFromContext returns the user previously stored in ctx by
// [ContextWithUser], or nil if no user is present.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey{}).(*User)
	return u
}
