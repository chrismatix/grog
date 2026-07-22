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
	writeResourceIdentityValue(hasher, resource.Label.String())
	writeResourceIdentityValue(hasher, resource.Up)
	writeResourceIdentityValue(hasher, resource.Down)
	writeResourceIdentityValue(hasher, resource.Ready)
	writeResourceIdentityValue(hasher, resource.GetTimeout().String())
	writeResourceIdentityValue(hasher, strconv.Itoa(len(resource.Dependencies)))
	for _, dependency := range resource.Dependencies {
		writeResourceIdentityValue(hasher, dependency.String())
	}

	exportKeys := make([]string, 0, len(resource.Exports))
	for key := range resource.Exports {
		exportKeys = append(exportKeys, key)
	}
	sort.Strings(exportKeys)
	writeResourceIdentityValue(hasher, strconv.Itoa(len(exportKeys)))
	for _, key := range exportKeys {
		writeResourceIdentityValue(hasher, key)
		writeResourceIdentityValue(hasher, resource.Exports[key])
	}

	sum := hasher.SumString()
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

func writeResourceIdentityValue(hasher Hasher, value string) {
	_, _ = hasher.WriteString(strconv.Itoa(len(value)))
	_, _ = hasher.WriteString(":")
	_, _ = hasher.WriteString(value)
}
