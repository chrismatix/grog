package hashing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
)

// HashStrings computes the xxhash hash of a list of string
func HashStrings(strList []string) string {
	// Sort the strings to ensure consistent outputs.
	sort.Strings(strList)
	return fmt.Sprintf("%x", xxhash.Sum64String(strings.Join(strList, ",")))
}
