package hashing

// HashString computes the configured hash of a string.
func HashString(str string) string {
	hasher := GetHasher()
	_, _ = hasher.WriteString(str)
	return hasher.SumString()
}

func HashBytes(bytes []byte) string {
	hasher := GetHasher()
	_, _ = hasher.Write(bytes)
	return hasher.SumString()
}
