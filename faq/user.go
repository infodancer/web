package faq

import (
	"context"

	"github.com/infodancer/web/faq/faquser"
)

// User is re-exported from [faquser.User] for ergonomic access from host
// applications. See the [faquser] package for full documentation.
type User = faquser.User

// UserResolver is re-exported from [faquser.UserResolver]. See [faquser]
// for full documentation.
type UserResolver = faquser.UserResolver

// UserResolverFunc is re-exported from [faquser.UserResolverFunc]. See
// [faquser] for full documentation.
type UserResolverFunc = faquser.UserResolverFunc

// ContextWithUser is re-exported from [faquser.ContextWithUser].
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return faquser.ContextWithUser(ctx, u)
}

// UserFromContext is re-exported from [faquser.UserFromContext].
func UserFromContext(ctx context.Context) *User {
	return faquser.UserFromContext(ctx)
}
