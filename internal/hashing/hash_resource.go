package hashing

import (
	"sort"
	"strconv"

	"grog/internal/model"
)

// GetResourceIdentity returns a short stable identifier derived from the
// resource's complete behavior-affecting definition.
func GetResourceIdentity(resource model.Resource) string {
	hasher := GetHasher()
	writeIdentityValue(hasher, resource.Label.String())
	writeIdentityValue(hasher, resource.Up)
	writeIdentityValue(hasher, resource.Down)
	writeIdentityValue(hasher, resource.Ready)
	writeIdentityValue(hasher, resource.GetTimeout().String())
	writeIdentityValue(hasher, strconv.Itoa(len(resource.Dependencies)))
	for _, dependency := range resource.Dependencies {
		writeIdentityValue(hasher, dependency.String())
	}

	exportKeys := make([]string, 0, len(resource.Exports))
	for key := range resource.Exports {
		exportKeys = append(exportKeys, key)
	}
	sort.Strings(exportKeys)
	writeIdentityValue(hasher, strconv.Itoa(len(exportKeys)))
	for _, key := range exportKeys {
		writeIdentityValue(hasher, key)
		writeIdentityValue(hasher, resource.Exports[key])
	}

	sum := hasher.SumString()
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

func writeIdentityValue(hasher Hasher, value string) {
	_, _ = hasher.WriteString(strconv.Itoa(len(value)))
	_, _ = hasher.WriteString(":")
	_, _ = hasher.WriteString(value)
}
