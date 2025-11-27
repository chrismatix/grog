package hashing

import (
	"sort"
	"strings"
)

// HashStrings computes the configured hash of a list of strings.
func HashStrings(strList []string) string {
	// Sort the strings to ensure consistent outputs.
	sort.Strings(strList)
	return HashString(strings.Join(strList, ","))
}
