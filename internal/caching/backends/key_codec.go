package backends

import (
	"encoding/base32"
	"strings"
)

const encodedKeyPrefix = "k-"

var cacheKeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// encodeCacheKey maps logical cache keys to portable physical storage keys.
func encodeCacheKey(key string) string {
	if isPortableCacheKey(key) {
		return key
	}
	return encodedKeyPrefix + strings.ToLower(cacheKeyEncoding.EncodeToString([]byte(key)))
}

func physicalCacheKey(key string) string {
	if key == "" {
		return ""
	}
	return encodeCacheKey(key)
}

func joinObjectPath(parts ...string) string {
	nonEmptyParts := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmedPart := strings.Trim(part, "/")
		if trimmedPart == "" {
			continue
		}
		nonEmptyParts = append(nonEmptyParts, trimmedPart)
	}
	return strings.Join(nonEmptyParts, "/")
}

// decodeCacheKey maps portable physical storage keys back to logical keys.
func decodeCacheKey(key string) (string, error) {
	if !strings.HasPrefix(key, encodedKeyPrefix) {
		return key, nil
	}
	decoded, err := cacheKeyEncoding.DecodeString(strings.ToUpper(strings.TrimPrefix(key, encodedKeyPrefix)))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func isPortableCacheKey(key string) bool {
	if key == "" || strings.HasPrefix(key, encodedKeyPrefix) {
		return false
	}

	segmentStart := 0
	for index := 0; index < len(key); index++ {
		character := key[index]
		switch {
		case character >= 'A' && character <= 'Z':
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '_' || character == '-' || character == '.':
		case character == '/':
			if !isPortableCacheKeySegment(key[segmentStart:index]) {
				return false
			}
			segmentStart = index + 1
		default:
			return false
		}
	}

	return isPortableCacheKeySegment(key[segmentStart:])
}

func isPortableCacheKeySegment(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return false
	}
	return !isWindowsReservedSegment(segment)
}

func isWindowsReservedSegment(segment string) bool {
	if baseSegment, _, hasExtension := strings.Cut(segment, "."); hasExtension {
		segment = baseSegment
	}
	if strings.EqualFold(segment, "CON") ||
		strings.EqualFold(segment, "PRN") ||
		strings.EqualFold(segment, "AUX") ||
		strings.EqualFold(segment, "NUL") {
		return true
	}
	if len(segment) == 4 {
		prefix := segment[:3]
		suffix := segment[3]
		if suffix >= '1' && suffix <= '9' && (strings.EqualFold(prefix, "COM") || strings.EqualFold(prefix, "LPT")) {
			return true
		}
	}
	return false
}
