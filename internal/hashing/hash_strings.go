package hashing

import (
	"fmt"
	"sort"
	"strings"
)

// HashStrings computes the xxhash hash of a list of string
func HashStrings(strList []string) string {
	// Sort the strings to ensure consistent outputs.
	sort.Strings(strList)
	return fmt.Sprintf("%x", HashString(strings.Join(strList, ",")))
}
