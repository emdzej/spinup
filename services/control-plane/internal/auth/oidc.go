package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/emdzej/spinup/services/control-plane/internal/config"
	"github.com/emdzej/spinup/services/control-plane/internal/session"
)

type Verifier struct {
	v        *oidc.IDTokenVerifier
	skipAuth bool
	// sessions lets Middleware accept the BFF cookie in addition to Bearer
	// tokens. Set by main after NewVerifier via SetSessions; the field stays
	// nil when only Bearer-token auth is wired (headless usage, tests).
	sessions session.Store
	// policy is the authorization check applied after identity resolution.
	// nil == permissive (every authenticated user allowed).
	policy *Policy
}

// SetSessions enables cookie-based auth on this Verifier. Passing nil disables
// the cookie path (Bearer-only behavior).
func (v *Verifier) SetSessions(s session.Store) { v.sessions = s }

// SetPolicy sets the authz policy used by Middleware. Passing nil disables
// authz (every authenticated user is allowed).
func (v *Verifier) SetPolicy(p *Policy) { v.policy = p }

type Claims struct {
	Subject string   `json:"sub"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups,omitempty"`
	// Roles is the OIDC `roles` claim — a flat array of strings. Used by the
	// authz layer; see internal/auth/authz.go. IdPs that emit roles under a
	// nested path (Keycloak's realm_access.roles) need a claim mapper to
	// project them onto top-level `roles`.
	Roles []string `json:"roles,omitempty"`
}

func NewVerifier(ctx context.Context, cfg config.OIDCConfig) (*Verifier, error) {
	if cfg.DevInsecureSkipAuth {
		return &Verifier{skipAuth: true}, nil
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	audience := cfg.Audience
	if audience == "" {
		audience = cfg.ClientID
	}
	return &Verifier{v: provider.Verifier(&oidc.Config{ClientID: audience})}, nil
}

// Middleware authenticates the request via either the BFF session cookie or a
// Bearer ID token, and attaches the resulting claims to the request context.
// Cookie takes precedence — browsers hitting the API via the UI never carry
// Bearer tokens.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v.skipAuth {
			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), Claims{Subject: "dev", Email: "dev@localhost"})))
			return
		}
		c, ok := v.resolve(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !v.policy.Authorize(c) {
			http.Error(w, "forbidden: missing required role", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), c)))
	})
}

type ctxKey struct{}

func withClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

func bearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return "", errors.New("missing bearer token")
	}
	return strings.TrimSpace(h[len(p):]), nil
}
