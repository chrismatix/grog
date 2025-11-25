package hashing

import "github.com/zeebo/xxh3"

// GetHasher returns a new hasher
// abstracted here to make testing new hash algorithms easier
func GetHasher() *xxh3.Hasher {
	return xxh3.New()
}
