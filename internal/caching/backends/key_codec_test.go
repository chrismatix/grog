package backends

import (
	"strings"
	"testing"
)

func TestCacheKeyCodecSafeKeysPassThrough(t *testing.T) {
	testCases := []string{
		"abc123",
		"trace-001.parquet",
		"traces/builds/2026-04-22/trace.parquet",
	}

	for _, key := range testCases {
		t.Run(key, func(t *testing.T) {
			physicalKey := encodeCacheKey(key)
			if physicalKey != key {
				t.Fatalf("encodeCacheKey(%q) = %q, want unchanged", key, physicalKey)
			}
			logicalKey, err := decodeCacheKey(physicalKey)
			if err != nil {
				t.Fatalf("decodeCacheKey(%q) returned error: %v", physicalKey, err)
			}
			if logicalKey != key {
				t.Fatalf("decodeCacheKey(%q) = %q, want %q", physicalKey, logicalKey, key)
			}
		})
	}
}

func TestCacheKeyCodecUnsafeKeysEncodeAndRoundTrip(t *testing.T) {
	testCases := []string{
		"sha256:deadbeef",
		"//pkg:target",
		"myorg/myapp:latest",
		"name with spaces",
		"CON",
		"CON.txt",
		"trailing.",
		"k-existing",
	}

	for _, key := range testCases {
		t.Run(key, func(t *testing.T) {
			physicalKey := encodeCacheKey(key)
			if !strings.HasPrefix(physicalKey, encodedKeyPrefix) {
				t.Fatalf("encodeCacheKey(%q) = %q, want %q prefix", key, physicalKey, encodedKeyPrefix)
			}
			if strings.Contains(physicalKey, "=") {
				t.Fatalf("encoded key %q contains padding", physicalKey)
			}
			for _, character := range strings.TrimPrefix(physicalKey, encodedKeyPrefix) {
				if character < 'a' || character > 'z' {
					if character < '2' || character > '7' {
						t.Fatalf("encoded key %q contains non-base32 character %q", physicalKey, character)
					}
				}
			}
			logicalKey, err := decodeCacheKey(physicalKey)
			if err != nil {
				t.Fatalf("decodeCacheKey(%q) returned error: %v", physicalKey, err)
			}
			if logicalKey != key {
				t.Fatalf("decodeCacheKey(%q) = %q, want %q", physicalKey, logicalKey, key)
			}
		})
	}
}
