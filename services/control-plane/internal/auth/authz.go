package auth

// Policy decides whether an authenticated user is authorized to use SpinUP.
// Authn (Verifier) tells us WHO the user is; Policy tells us whether they're
// allowed in. Keeping them separate lets /auth/me distinguish "no session"
// (401 → send to /auth/login) from "session but not entitled" (200 with
// authorized:false → UI renders a "contact admin" screen).
type Policy struct {
	// requiredRoles is an any-of match against the `roles` claim. Empty means
	// "authenticated is sufficient" — every logged-in user is authorized.
	requiredRoles []string
}

func NewPolicy(requiredRoles []string) *Policy {
	// Copy to detach from caller's slice.
	rr := make([]string, 0, len(requiredRoles))
	for _, r := range requiredRoles {
		if r != "" {
			rr = append(rr, r)
		}
	}
	return &Policy{requiredRoles: rr}
}

// Authorize returns true if the claims satisfy the policy.
func (p *Policy) Authorize(c Claims) bool {
	if p == nil || len(p.requiredRoles) == 0 {
		return true
	}
	for _, need := range p.requiredRoles {
		for _, got := range c.Roles {
			if got == need {
				return true
			}
		}
	}
	return false
}
