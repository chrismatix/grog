// Package flagtypes provides reusable pflag value types.
package flagtypes

import (
	"fmt"
	"slices"
	"strings"
)

// Enum is a string flag value restricted to a fixed set of values.
type Enum struct {
	Value   string
	Allowed []string
}

// NewEnum creates an enum whose default is also its first allowed value.
func NewEnum(defaultValue string, additionalAllowed ...string) *Enum {
	allowed := append([]string{defaultValue}, additionalAllowed...)
	return &Enum{Value: defaultValue, Allowed: allowed}
}

// String returns the current value.
func (enum *Enum) String() string {
	return enum.Value
}

// Set validates and stores a value.
func (enum *Enum) Set(value string) error {
	if !slices.Contains(enum.Allowed, value) {
		return fmt.Errorf("must be one of: %s", strings.Join(enum.Allowed, ", "))
	}
	enum.Value = value
	return nil
}

// Type returns the value type displayed by pflag.
func (enum *Enum) Type() string {
	return "string"
}
