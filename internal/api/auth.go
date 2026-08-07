package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type principalKey struct{}

// Role is an API authorization role.
type Role string

const (
	// RoleReader permits read-only operational access.
	RoleReader Role = "reader"
	// RoleOperator permits equipment commands and alarm acknowledgement.
	RoleOperator Role = "operator"
	// RoleAdministrator permits configuration and administrative operations.
	RoleAdministrator Role = "administrator"
)

// Principal is an authenticated API identity.
type Principal struct {
	ID    string `json:"id"`
	Roles []Role `json:"roles"`
}

// Authenticator authenticates one HTTP request without embedding policy in a handler.
type Authenticator interface {
	Authenticate(*http.Request) (Principal, error)
}

// Authorizer decides whether a principal may perform an API action.
type Authorizer interface {
	Authorize(context.Context, Principal, Role) error
}

// ErrUnauthorized is returned for invalid or absent credentials.
var ErrUnauthorized = errors.New("authentication required")

// ErrForbidden is returned when an authenticated principal lacks a role.
var ErrForbidden = errors.New("operation is not authorized")

// BearerAuthenticator authenticates one externally supplied bearer token.
// It is intentionally small and can later be replaced by a stronger provider.
type BearerAuthenticator struct {
	token     string
	principal Principal
}

// NewBearerAuthenticator constructs a constant-time bearer authenticator.
func NewBearerAuthenticator(token string, principal Principal) (*BearerAuthenticator, error) {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(principal.ID) == "" {
		return nil, errors.New("bearer token and principal ID are required")
	}
	return &BearerAuthenticator{token: token, principal: principal}, nil
}

// Authenticate verifies an RFC 6750 Authorization header.
func (a *BearerAuthenticator) Authenticate(request *http.Request) (Principal, error) {
	const prefix = "Bearer "
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return Principal{}, ErrUnauthorized
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if len(provided) != len(a.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(a.token)) != 1 {
		return Principal{}, ErrUnauthorized
	}
	return a.principal, nil
}

type denyAuthenticator struct{}

func (denyAuthenticator) Authenticate(*http.Request) (Principal, error) {
	return Principal{}, ErrUnauthorized
}

type roleAuthorizer struct{}

func (roleAuthorizer) Authorize(_ context.Context, principal Principal, required Role) error {
	for _, role := range principal.Roles {
		if role == RoleAdministrator || role == required || (role == RoleOperator && required == RoleReader) {
			return nil
		}
	}
	return ErrForbidden
}

func principalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}
