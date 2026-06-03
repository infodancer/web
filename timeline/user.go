package timeline

import (
	"context"

	"github.com/infodancer/web/timeline/timelineuser"
)

// User is re-exported from [timelineuser.User] for ergonomic access from host
// applications.
type User = timelineuser.User

// UserResolver is re-exported from [timelineuser.UserResolver].
type UserResolver = timelineuser.UserResolver

// UserResolverFunc is re-exported from [timelineuser.UserResolverFunc].
type UserResolverFunc = timelineuser.UserResolverFunc

// ContextWithUser is re-exported from [timelineuser.ContextWithUser].
func ContextWithUser(ctx context.Context, u *User) context.Context {
	return timelineuser.ContextWithUser(ctx, u)
}

// UserFromContext is re-exported from [timelineuser.UserFromContext].
func UserFromContext(ctx context.Context) *User {
	return timelineuser.UserFromContext(ctx)
}
