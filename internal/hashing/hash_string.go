package hashing

import (
	"fmt"
	"github.com/cespare/xxhash/v2"
)

// HashString computes the xxhash hash of a string
func HashString(str string) string {
	return fmt.Sprintf("%x", xxhash.Sum64String(str))
}
