package output

import (
	"fmt"
	"grog/internal/hashing"
	"grog/internal/proto/gen"
	"sort"

	"google.golang.org/protobuf/proto"
)

func getOutputHash(outputs []*gen.Output) (string, error) {
	if len(outputs) == 0 {
		return "", nil
	}

	marshalOptions := proto.MarshalOptions{Deterministic: true}
	// Calculate combined hash
	digests := make([]string, 0, len(outputs))
	for _, output := range outputs {
		var digest string
		data, err := marshalOptions.Marshal(output)
		if err != nil {
			return "", fmt.Errorf("failed to marshal output: %w", err)
		}
		digest = hashing.HashBytes(data)
		digests = append(digests, digest)
	}

	hasher := hashing.GetHasher()
	// Sort digests to ensure a consistent order
	sort.Sort(sort.StringSlice(digests))
	for _, digest := range digests {
		_, err := hasher.WriteString(digest)
		if err != nil {
			return "", err
		}
	}

	return hasher.SumString(), nil
}
