package hashing

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"

	"grog/internal/config"

	"github.com/zeebo/xxh3"
)

// Hasher defines the hashing interface used throughout grog.
type Hasher interface {
	io.Writer
	WriteString(string) (int, error)
	SumString() string
}

// GetHasher returns a new hasher instance based on the configured algorithm.
// xxh3 is the default for speed, but sha256 can be selected for a
// cryptographic-grade hash.
func GetHasher() Hasher {
	switch config.Global.HashAlgorithm {
	case config.HashAlgorithmSHA256:
		return newSHA256Hasher()
	case "", config.HashAlgorithmXXH3:
		fallthrough
	default:
		return newXXH3Hasher()
	}
}

type xxh3Hasher struct {
	hasher *xxh3.Hasher
}

func newXXH3Hasher() Hasher {
	return &xxh3Hasher{hasher: xxh3.New()}
}

func (h *xxh3Hasher) Write(p []byte) (int, error) {
	return h.hasher.Write(p)
}

func (h *xxh3Hasher) WriteString(s string) (int, error) {
	return h.hasher.WriteString(s)
}

func (h *xxh3Hasher) SumString() string {
	sum128 := h.hasher.Sum128()
	return fmt.Sprintf("%016x%016x", sum128.Hi, sum128.Lo)
}

type sha256Hasher struct {
	hasher hash.Hash
}

func newSHA256Hasher() Hasher {
	return &sha256Hasher{hasher: sha256.New()}
}

func (h *sha256Hasher) Write(p []byte) (int, error) {
	return h.hasher.Write(p)
}

func (h *sha256Hasher) WriteString(s string) (int, error) {
	return io.WriteString(h.hasher, s)
}

func (h *sha256Hasher) SumString() string {
	return fmt.Sprintf("%016x", h.hasher.Sum(nil))
}

// Assert types
var _ Hasher = (*xxh3Hasher)(nil)
var _ Hasher = (*sha256Hasher)(nil)
