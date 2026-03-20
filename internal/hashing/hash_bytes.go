package hashing

func HashBytes(bytes []byte) string {
	hasher := GetHasher()
	_, _ = hasher.Write(bytes)
	return hasher.SumString()
}
