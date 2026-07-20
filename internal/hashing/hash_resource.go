package hashing

import "grog/internal/model"

// GetResourceIdentity returns a short stable identifier for a resource derived
// from its label and definition. Editing the definition changes the identity,
// so lifecycle commands built on it (e.g. container names) never adopt an
// instance started from a stale definition.
func GetResourceIdentity(resource model.Resource) string {
	hasher := GetHasher()
	_, _ = hasher.WriteString(resource.Label.String())
	_, _ = hasher.WriteString(resource.Up)
	_, _ = hasher.WriteString(resource.Down)
	_, _ = hasher.WriteString(resource.Ready)
	_, _ = hasher.WriteString(sortedKeyValue(resource.Exports))

	sum := hasher.SumString()
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}
