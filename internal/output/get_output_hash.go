package output

import (
	"fmt"
	"grog/internal/proto/gen"
	"sort"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/protobuf/proto"
)

func getOutputHash(outputs []*gen.Output) (string, error) {
	if len(outputs) == 0 {
		return "", nil
	}

	marshalOptions := proto.MarshalOptions{Deterministic: true}
	// Calculate combined hash
	digests := make([]string, len(outputs))
	for _, output := range outputs {
		var digest string
		data, err := marshalOptions.Marshal(output)
		if err != nil {
			return "", fmt.Errorf("failed to marshal output: %w", err)
		}
		digest = fmt.Sprintf("%x", xxhash.Sum64(data))
		digests = append(digests, digest)
	}

	hasher := xxhash.New()
	// Sort digests to ensure a consistent order
	sort.Sort(sort.StringSlice(digests))
	for _, digest := range digests {
		_, err := hasher.WriteString(digest)
		if err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%x", hasher.Sum64()), nil
}
