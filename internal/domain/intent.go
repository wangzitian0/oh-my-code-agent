package domain

import "fmt"

// Intent is the desired-state activation intent for one asset in one scope
// (docs/product/requirements.md §4, init.md "Activation Intent").
type Intent string

const (
	IntentRequired  Intent = "REQUIRED"
	IntentDefault   Intent = "DEFAULT"
	IntentAvailable Intent = "AVAILABLE"
	IntentDenied    Intent = "DENIED"
)

// Valid reports whether i is one of the four defined intents.
func (i Intent) Valid() bool {
	switch i {
	case IntentRequired, IntentDefault, IntentAvailable, IntentDenied:
		return true
	default:
		return false
	}
}

// ValidateIntent rejects any value outside the closed intent enum.
func ValidateIntent(i Intent) error {
	if !i.Valid() {
		return fmt.Errorf("invalid intent %q", i)
	}
	return nil
}
