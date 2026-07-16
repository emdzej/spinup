package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/emdzej/spinup/services/control-plane/internal/config"
	"github.com/emdzej/spinup/services/control-plane/internal/session"
)

const (
	// CookieName is the browser session cookie. Opaque 256-bit session ID,
	// stored server-side.
	CookieName = "spinup_session"

	// Short-lived cookies used only during the OAuth dance. They carry the
	// PKCE verifier + state + returnTo so we don't need server-side state
	// between /auth/login and /auth/callback.
	stateCookieName    = "spinup_oauth_state"
	verifierCookieName = "spinup_oauth_verifier"
	returnToCookieName = "spinup_oauth_return"

	// OAuth intermediate cookies live 10 minutes — enough for slow humans,
	// short enough to avoid replay windows.
	oauthCookieTTL = 10 * time.Minute
)

// OAuth wires the OIDC code-flow-with-PKCE endpoints. It's a peer to Verifier,
// not a subtype — Verifier just checks tokens, OAuth orchestrates login.
type OAuth struct {
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	cfg          *oauth2.Config
	sessions     session.Store
	cookieSecure bool
	// If empty, /auth/logout only clears the local session. When non-empty
	// (the provider advertises end_session_endpoint), we also redirect to
	// terminate the SSO session upstream.
	endSessionURL string
}

// NewOAuth constructs the OIDC OAuth handler. Returns (nil, nil) when
// DevInsecureSkipAuth is on — callers should skip mounting auth routes.
func NewOAuth(ctx context.Context, cfg config.OIDCConfig, sessions session.Store) (*OAuth, error) {
	if cfg.DevInsecureSkipAuth {
		return nil, nil
	}
	if cfg.ClientSecret == "" {
		return nil, errors.New("SPINUP_OIDC_CLIENT_SECRET is required for the login flow")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("SPINUP_OIDC_REDIRECT_URL is required (e.g. https://spinup.example.com/auth/callback)")
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	audience := cfg.Audience
	if audience == "" {
		audience = cfg.ClientID
	}
	// Best-effort: pull end_session_endpoint from provider metadata.
	var endSession struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&endSession)

	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.RedirectURL,
		// Session lifetime tracks the ID token's exp; we never refresh, so we
		// skip offline_access to avoid holding a refresh token we don't need
		// (and to skip the "wants offline access" consent prompt some IdPs show).
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return &OAuth{
		provider:      provider,
		verifier:      provider.Verifier(&oidc.Config{ClientID: audience}),
		cfg:           oc,
		sessions:      sessions,
		cookieSecure:  strings.HasPrefix(cfg.RedirectURL, "https://"),
		endSessionURL: endSession.EndSessionEndpoint,
	}, nil
}

// Register mounts the auth routes on the given mux. In dev-skip mode
// (oa == nil), only /auth/me is registered so the UI's bootstrap works.
func Register(mux *http.ServeMux, v *Verifier, oa *OAuth) {
	if oa == nil {
		// Dev-skip: /auth/me returns a synthetic user so the UI can proceed.
		mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, meResponse{
				Subject:    "dev",
				Email:      "dev@localhost",
				Authorized: true,
			})
		})
		// /auth/login and /auth/logout stubs so the UI's redirects don't 404.
		mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, returnToOrRoot(r), http.StatusFound)
		})
		mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		return
	}

	mux.HandleFunc("GET /auth/login", oa.handleLogin)
	mux.HandleFunc("GET /auth/callback", oa.handleCallback)
	mux.HandleFunc("POST /auth/logout", oa.handleLogout)
	mux.HandleFunc("GET /auth/me", handleMe(v))
}

func (o *OAuth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomHex(32)
	if err != nil {
		http.Error(w, "state: "+err.Error(), http.StatusInternalServerError)
		return
	}
	verifier, err := randomHex(32)
	if err != nil {
		http.Error(w, "verifier: "+err.Error(), http.StatusInternalServerError)
		return
	}
	challenge := pkceChallenge(verifier)

	o.setShortCookie(w, stateCookieName, state)
	o.setShortCookie(w, verifierCookieName, verifier)
	o.setShortCookie(w, returnToCookieName, returnToOrRoot(r))

	authURL := o.cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (o *OAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Compare state.
	stateFromQuery := r.URL.Query().Get("state")
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateFromQuery != stateCookie.Value {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	// PKCE verifier.
	verifierCookie, err := r.Cookie(verifierCookieName)
	if err != nil || verifierCookie.Value == "" {
		http.Error(w, "missing pkce verifier", http.StatusBadRequest)
		return
	}
	// Exchange the auth code.
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	token, err := o.cfg.Exchange(r.Context(), code,
		oauth2.SetAuthURLParam("code_verifier", verifierCookie.Value),
	)
	if err != nil {
		http.Error(w, "token exchange: "+err.Error(), http.StatusBadGateway)
		return
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		http.Error(w, "no id_token in response", http.StatusBadGateway)
		return
	}
	idt, err := o.verifier.Verify(r.Context(), rawID)
	if err != nil {
		http.Error(w, "verify id_token: "+err.Error(), http.StatusUnauthorized)
		return
	}
	var claims Claims
	if err := idt.Claims(&claims); err != nil {
		http.Error(w, "decode claims: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Create session, set cookie.
	sid, err := randomHex(32)
	if err != nil {
		http.Error(w, "session id: "+err.Error(), http.StatusInternalServerError)
		return
	}
	expires := idt.Expiry
	if expires.IsZero() {
		expires = time.Now().Add(time.Hour)
	}
	if err := o.sessions.Create(r.Context(), session.Session{
		ID:          sid,
		Subject:     claims.Subject,
		Email:       claims.Email,
		Groups:      claims.Groups,
		Roles:       claims.Roles,
		CreatedAt:   time.Now(),
		ExpiresAt:   expires,
		AccessToken: token.AccessToken,
	}); err != nil {
		http.Error(w, "store session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sid,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	// Consume the intermediate cookies.
	o.clearShortCookies(w)

	returnTo := "/"
	if rt, err := r.Cookie(returnToCookieName); err == nil && strings.HasPrefix(rt.Value, "/") {
		returnTo = rt.Value
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (o *OAuth) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = o.sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"endSessionUrl": o.endSessionURL, // may be empty; UI decides whether to visit
	})
}

// handleMe returns the current user's identity + whether they're authorized.
// Note: 401 vs 200-with-authorized:false is intentional. 401 = "no session,
// send them to /auth/login." 200 authorized:false = "logged in but missing
// the required role — show the not-authorized screen with a Logout button."
func handleMe(v *Verifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := v.resolve(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, meResponse{Anonymous: true})
			return
		}
		writeJSON(w, http.StatusOK, meResponse{
			Subject:    claims.Subject,
			Email:      claims.Email,
			Groups:     claims.Groups,
			Roles:      claims.Roles,
			Authorized: v.policy.Authorize(claims),
		})
	}
}

// resolve pulls Claims from the request via either the session cookie or a
// Bearer token. Public for /auth/me and other same-file consumers.
func (v *Verifier) resolve(r *http.Request) (Claims, bool) {
	if v.skipAuth {
		return Claims{Subject: "dev", Email: "dev@localhost"}, true
	}
	if v.sessions != nil {
		if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
			if s, ok := v.sessions.Get(r.Context(), c.Value); ok {
				return Claims{Subject: s.Subject, Email: s.Email, Groups: s.Groups, Roles: s.Roles}, true
			}
		}
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(h[len("Bearer "):])
		if idt, err := v.v.Verify(r.Context(), token); err == nil {
			var c Claims
			if err := idt.Claims(&c); err == nil {
				return c, true
			}
		}
	}
	return Claims{}, false
}

type meResponse struct {
	Subject    string   `json:"sub,omitempty"`
	Email      string   `json:"email,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Roles      []string `json:"roles,omitempty"`
	Anonymous  bool     `json:"anonymous,omitempty"`
	Authorized bool     `json:"authorized"`
}

// Helpers.

func (o *OAuth) setShortCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(oauthCookieTTL),
		HttpOnly: true,
		Secure:   o.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (o *OAuth) clearShortCookies(w http.ResponseWriter) {
	for _, name := range []string{stateCookieName, verifierCookieName, returnToCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   o.cookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func returnToOrRoot(r *http.Request) string {
	rt := r.URL.Query().Get("returnTo")
	// Only allow same-origin returnTo values to prevent open-redirect abuse.
	if strings.HasPrefix(rt, "/") && !strings.HasPrefix(rt, "//") {
		if _, err := url.Parse(rt); err == nil {
			return rt
		}
	}
	return "/"
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

