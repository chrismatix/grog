package hashing

import (
	"fmt"

	"github.com/zeebo/xxh3"
)

// HashString computes the xxhash hash of a string
func HashString(str string) string {
	return fmt.Sprintf("%x", xxh3.HashString(str))
}

func HashBytes(bytes []byte) string {
	return fmt.Sprintf("%x", xxh3.Hash128(bytes))
}
