package profile

import (
	"context"

	"github.com/infodancer/web/profile/profileuser"
)

// User is re-exported from [profileuser.User] for ergonomic access from host
// applications.
type User = profileuser.User

// Resolver is re-exported from [profileuser.Resolver].
type Resolver = profileuser.Resolver

// ResolverFunc is re-exported from [profileuser.ResolverFunc].
type ResolverFunc = profileuser.ResolverFunc

// ContextWithUser is re-exported from [profileuser.ContextWithUser].
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return profileuser.ContextWithUser(ctx, u)
}

// UserFromContext is re-exported from [profileuser.UserFromContext].
func UserFromContext(ctx context.Context) *User {
	return profileuser.UserFromContext(ctx)
}
