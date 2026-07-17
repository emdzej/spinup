// Package policy owns platform-level constraints applied to application
// config. Right now it's just resource caps; add more here (allowed images,
// allowed outbound hosts, secret sourcing rules …) rather than sprinkling
// validation across handlers.
package policy

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Mode picks how the platform's resource values interact with the user's.
type Mode string

const (
	// ModeOpen — user's values are used verbatim; platform values ignored.
	// Default when no policy is configured.
	ModeOpen Mode = "open"
	// ModeMax — user's values must be <= platform's per-field cap. A user
	// value <= "" (unset) is always allowed; the platform cap acts as an
	// upper bound only, not a floor. Unset platform fields are unlimited.
	ModeMax Mode = "max"
	// ModeForced — platform's values win; anything the user sends is
	// replaced. UI should render the inputs disabled.
	ModeForced Mode = "forced"
)

// Resources is the four-field resource shape used across store/spinapp/DTO.
// Duplicated here to avoid importing store from policy (policy is a leaf).
type Resources struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
}

// ResourcePolicy carries the platform's per-field caps + the mode that
// decides how they interact with the user's input.
type ResourcePolicy struct {
	Mode      Mode
	CPU, Mem  Range
}

// Range is [request, limit] per resource kind (k8s quantity strings).
type Range struct {
	Request string
	Limit   string
}

// ErrPolicyViolation is returned when the user's value exceeds a cap. The
// message names the field so the UI can point at the offending input.
type ErrPolicyViolation struct{ Field, User, Cap string }

func (e *ErrPolicyViolation) Error() string {
	return fmt.Sprintf("%s: %q exceeds platform cap %q", e.Field, e.User, e.Cap)
}

// Apply cleans the caller's Resources per the configured mode.
//
//   ModeOpen   → returns user's values unchanged.
//   ModeForced → returns the platform's values; user's are discarded.
//   ModeMax    → returns user's values, ErrPolicyViolation if any field
//                exceeds its cap. Empty user fields are always allowed.
//
// Called both by the PATCH handler (for early rejection) and by the
// deployer (as a safety net when policy tightens after apps were stored).
func (p ResourcePolicy) Apply(user Resources) (Resources, error) {
	switch p.Mode {
	case "", ModeOpen:
		return user, nil
	case ModeForced:
		return Resources{
			CPURequest:    p.CPU.Request,
			CPULimit:      p.CPU.Limit,
			MemoryRequest: p.Mem.Request,
			MemoryLimit:   p.Mem.Limit,
		}, nil
	case ModeMax:
		if err := checkMax("cpuRequest", user.CPURequest, p.CPU.Request); err != nil {
			return Resources{}, err
		}
		if err := checkMax("cpuLimit", user.CPULimit, p.CPU.Limit); err != nil {
			return Resources{}, err
		}
		if err := checkMax("memoryRequest", user.MemoryRequest, p.Mem.Request); err != nil {
			return Resources{}, err
		}
		if err := checkMax("memoryLimit", user.MemoryLimit, p.Mem.Limit); err != nil {
			return Resources{}, err
		}
		return user, nil
	default:
		return Resources{}, fmt.Errorf("unknown resources policy mode %q", p.Mode)
	}
}

// checkMax returns nil if user is empty, cap is empty (= unlimited), or the
// parsed quantities satisfy user <= cap.
func checkMax(field, user, cap string) error {
	if user == "" || cap == "" {
		return nil
	}
	uq, err := resource.ParseQuantity(user)
	if err != nil {
		return fmt.Errorf("%s: invalid quantity %q: %w", field, user, err)
	}
	cq, err := resource.ParseQuantity(cap)
	if err != nil {
		// Bad platform config — surface loudly rather than silently
		// accepting whatever the user sent.
		return fmt.Errorf("%s: platform cap %q is invalid: %w", field, cap, err)
	}
	if uq.Cmp(cq) > 0 {
		return &ErrPolicyViolation{Field: field, User: user, Cap: cap}
	}
	return nil
}

// IsPolicyViolation lets callers distinguish "user picked too much" (400) from
// "misconfiguration" (500) without depending on the concrete error type.
func IsPolicyViolation(err error) bool {
	var pv *ErrPolicyViolation
	return errors.As(err, &pv)
}
