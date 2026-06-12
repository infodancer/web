// Package bloguser defines the identity contract between the blog module
// and its host. The module owns no users, sessions, or login flow: the host
// supplies a [Resolver] that turns an incoming request into a [*User], and
// the module trusts it completely. The roles the User carries (Author,
// Admin) are the host's to assign — the blogauthz sibling package adapts
// the infodancer/authz role store into this shape so authorship stays
// invitation-only via role grants.
package bloguser

import (
	"context"
	"net/http"
)

// User is an authenticated visitor as the host resolved it.
type User struct {
	// Key is the host's stable, opaque user identifier (the same key the
	// host's profile component uses, e.g. users.id rendered as text). Posts
	// record it as author_key; it is never displayed.
	Key string

	// DisplayName seeds the author registry's display name for users the
	// AuthorDirectory hook doesn't know (or when no hook is configured).
	DisplayName string

	// Author grants post creation and editing of the user's own posts.
	// Authorship is invitation-only: the host sets this from a role grant
	// (authz module "blog", role "author"), never from self-registration.
	Author bool

	// Admin grants editing any post, all lifecycle transitions on any post,
	// and named-blog management.
	Admin bool
}

// Resolver translates an incoming request into a [*User].
//
// Returning (nil, nil) means anonymous — read-only access to published
// posts. Returning (nil, err) means a credential was present but invalid;
// the module responds 401.
type Resolver interface {
	Resolve(ctx context.Context, r *http.Request) (*User, error)
}

// ResolverFunc adapts a function to the [Resolver] interface.
type ResolverFunc func(ctx context.Context, r *http.Request) (*User, error)

// Resolve implements [Resolver].
func (f ResolverFunc) Resolve(ctx context.Context, r *http.Request) (*User, error) {
	return f(ctx, r)
}

type contextKey struct{}

// ContextWithUser returns a context carrying u. The module's middleware
// stores the resolved user here once per request.
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}

// UserFromContext returns the user stored by ContextWithUser, or nil for
// anonymous requests.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(contextKey{}).(*User)
	return u
}
