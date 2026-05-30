// Package faqauthz adapts the infodancer/authz role store into faq's
// [faquser.UserResolver]. It is the bridge that lets a host grant faq
// moderation rights through authz — a local role store keyed on
// (issuer, subject, module, role) — instead of trusting a JWT role claim.
//
// authz itself has no knowledge of HTTP. This adapter owns the
// request→identity step: the host supplies an [Identifier] (a thin
// wrapper over its existing auth-N, e.g. an oidclient session) and an
// [authz.Store]. The host does not implement faquser.UserResolver
// itself, and it does not read role claims from tokens. See faq's
// DESIGN.md §7.
package faqauthz

import (
	"context"
	"net/http"

	"github.com/infodancer/authz"
	"github.com/infodancer/web/faq/faquser"
)

// Module is the authz module name faq roles are scoped under. A user is a
// faq admin if they hold [AdminRole] in this module or globally (authz
// treats the empty module as global).
const Module = "faq"

// AdminRole is the authz role that grants faq moderation privileges
// (close, delete, override the edit grace period).
const AdminRole = "admin"

// Identifier extracts an authenticated identity from a request. The host
// implements it over whatever auth-N it runs. Returning (nil, nil) means
// anonymous — read-only access; returning (nil, err) means a present-but-
// invalid credential, which faq turns into a 401.
type Identifier func(*http.Request) (*authz.Identity, error)

// NewResolver returns a [faquser.UserResolver] that identifies each
// request via identify and looks up the identity's roles in store, scoped
// to the faq module. The resulting user's Admin bit is set when the
// principal holds [AdminRole].
func NewResolver(identify Identifier, store authz.Store) faquser.UserResolver {
	r := &authz.Resolver{Store: store, Module: Module}
	return faquser.UserResolverFunc(func(ctx context.Context, req *http.Request) (*faquser.User, error) {
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
		return &faquser.User{
			Subject:     p.Subject,
			Issuer:      p.Issuer,
			DisplayName: p.DisplayName,
			Email:       p.Email,
			Admin:       p.HasRole(AdminRole),
		}, nil
	})
}
