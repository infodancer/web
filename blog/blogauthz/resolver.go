// Package blogauthz adapts the infodancer/authz role store into blog's
// [bloguser.Resolver]. It is the bridge that keeps authorship
// invitation-only: a user may write posts iff an admin has granted them
// the author role in the authz store (module "blog"), via authzctl or the
// host's own role UI. Blog ships no registration flow.
//
// authz itself has no knowledge of HTTP. This adapter owns the
// request→identity step: the host supplies an [Identifier] (a thin wrapper
// over its existing auth-N, e.g. an oidclient session) and an
// [authz.Store]. It mirrors faq's faqauthz package.
package blogauthz

import (
	"context"
	"net/http"

	"github.com/infodancer/authz"

	"github.com/infodancer/web/blog/bloguser"
)

// Module is the authz module name blog roles are scoped under. Global
// grants (empty module) also apply, per authz's resolver semantics.
const Module = "blog"

// AuthorRole grants post creation and editing of the holder's own posts.
// Granting it is the "invitation" in invitation-only authorship.
const AuthorRole = "author"

// AdminRole grants editing any post and managing named blogs.
const AdminRole = "admin"

// Identifier extracts an authenticated identity from a request. The host
// implements it over whatever auth-N it runs. Returning (nil, nil) means
// anonymous — read-only access; returning (nil, err) means a present-but-
// invalid credential, which blog turns into a 401.
type Identifier func(*http.Request) (*authz.Identity, error)

// KeyFunc maps an authenticated identity to the host's stable user key —
// the same key the host's profile component uses (e.g. users.id as text).
type KeyFunc func(id *authz.Identity) string

// NewResolver returns a [bloguser.Resolver] that identifies each request
// via identify, looks up the identity's roles in store scoped to the blog
// module, and maps the identity to a user key via key. A nil key falls
// back to issuer + "|" + subject, which is stable but host-opaque — hosts
// that key profiles by their own user id should pass their mapping.
func NewResolver(identify Identifier, store authz.Store, key KeyFunc) bloguser.Resolver {
	if key == nil {
		key = func(id *authz.Identity) string { return id.Issuer + "|" + id.Subject }
	}
	r := &authz.Resolver{Store: store, Module: Module}
	return bloguser.ResolverFunc(func(ctx context.Context, req *http.Request) (*bloguser.User, error) {
		id, err := identify(req)
		if err != nil {
			return nil, err
		}
		if id == nil {
			return nil, nil
		}
		p, err := r.Resolve(ctx, *id)
		if err != nil {
			return nil, err
		}
		return &bloguser.User{
			Key:         key(id),
			DisplayName: p.DisplayName,
			Author:      p.HasRole(AuthorRole),
			Admin:       p.HasRole(AdminRole),
		}, nil
	})
}
