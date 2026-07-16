// Package session stores authenticated user sessions server-side. The CP
// hands out an opaque session ID via HttpOnly cookie; every request that
// carries the cookie is resolved to Claims by Store.Get.
//
// The interface is intentionally small so we can later swap the in-memory
// impl for a SQLite/Postgres/Redis-backed one without touching callers.
package session

import (
	"context"
	"time"
)

// Session is one authenticated browser tab / API client. ID is the opaque
// cookie value; the identity fields are the user info we extracted from the
// ID token at login time.
//
// Identity is stored as flat fields rather than a nested auth.Claims to keep
// this package a leaf — auth depends on session, not the other way around.
type Session struct {
	ID        string
	Subject   string
	Email     string
	Groups    []string
	// Roles must survive session storage — the authz Policy runs on every
	// request and re-reads them via Verifier.resolve. Dropping Roles here
	// would silently deny every logged-in user.
	Roles     []string
	CreatedAt time.Time
	ExpiresAt time.Time
	// AccessToken is retained so we can call the OIDC end-session endpoint
	// on logout when the provider supports it. Not sent to clients.
	AccessToken string
}

// Store persists sessions. Implementations must be safe for concurrent use.
type Store interface {
	// Create stores a new session and returns it. Callers pre-populate ID,
	// Claims, ExpiresAt, and optionally AccessToken.
	Create(ctx context.Context, s Session) error

	// Get returns the session by ID, or (Session{}, false) if not found or expired.
	// Implementations should transparently delete expired sessions.
	Get(ctx context.Context, id string) (Session, bool)

	// Delete removes a session (logout). Missing IDs are not an error.
	Delete(ctx context.Context, id string) error
}
