package hashing

// HashString computes the configured hash of a string.
func HashString(str string) string {
	hasher := GetHasher()
	_, _ = hasher.WriteString(str)
	return hasher.SumString()
}
