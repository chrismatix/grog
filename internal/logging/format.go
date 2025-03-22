package logging

import "fmt"

// Pl small pluralization helper
func Pl(s string, count int) string {
	if count == 1 {
		return s
	}
	return s + "s"
}

// FCount Format a count
func FCount(count int, s string) string {
	return fmt.Sprintf("%d %s", count, Pl(s, count))
}

// FCountTargets Format a target count
func FCountTargets(count int) string {
	return FCount(count, "target")
}

// FCountPkg Format a package count
func FCountPkg(count int) string {
	return FCount(count, "package")
}
