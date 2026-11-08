package auth

import (
	"context"
	"time"
)

// MachineIdentity describes an authenticated machine client.
type MachineIdentity struct {
	APIKeyID string
}

// UserIdentity describes an authenticated JWT user.
type UserIdentity struct {
	Subject   string
	Issuer    string
	Audiences []string
	Claims    map[string]interface{}
	ExpiresAt time.Time
	NotBefore time.Time
}

// Context is the authentication result attached to a request context.
type Context struct {
	Machine *MachineIdentity
	User    *UserIdentity
}

type contextKey struct{}

// WithContext attaches authentication results to ctx.
func WithContext(ctx context.Context, authCtx *Context) context.Context {
	return context.WithValue(ctx, contextKey{}, authCtx)
}

// FromContext extracts authentication results from ctx.
func FromContext(ctx context.Context) (*Context, bool) {
	authCtx, ok := ctx.Value(contextKey{}).(*Context)
	return authCtx, ok && authCtx != nil
}
