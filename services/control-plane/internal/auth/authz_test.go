package auth

import "testing"

func TestPolicy(t *testing.T) {
	cases := []struct {
		name     string
		required []string
		claims   Claims
		want     bool
	}{
		{name: "no policy allows everything", required: nil, claims: Claims{}, want: true},
		{name: "empty required allows everything", required: []string{}, claims: Claims{}, want: true},
		{name: "single role matches", required: []string{"spinup"}, claims: Claims{Roles: []string{"spinup"}}, want: true},
		{name: "single role missing", required: []string{"spinup"}, claims: Claims{Roles: []string{"other"}}, want: false},
		{name: "any-of matches", required: []string{"admin", "spinup"}, claims: Claims{Roles: []string{"spinup"}}, want: true},
		{name: "empty claim never authorized", required: []string{"spinup"}, claims: Claims{Roles: nil}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPolicy(tc.required)
			if got := p.Authorize(tc.claims); got != tc.want {
				t.Fatalf("Authorize(%+v) with required=%v: got %v want %v", tc.claims, tc.required, got, tc.want)
			}
		})
	}

	t.Run("nil receiver is permissive", func(t *testing.T) {
		var p *Policy
		if !p.Authorize(Claims{}) {
			t.Fatal("nil policy should authorize everything")
		}
	})
}
