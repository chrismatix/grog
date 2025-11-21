package output

import (
	"fmt"
	"grog/internal/proto/gen"
	"sort"

	"github.com/cespare/xxhash/v2"
)

func getOutputHash(outputs []*gen.Output) (string, error) {
	sortOutputs(outputs)

	// Calculate combined hash
	hasher := xxhash.New()
	for _, output := range outputs {
		var digest string
		switch o := output.Kind.(type) {
		case *gen.Output_File:
			digest = o.File.Digest.Hash
		case *gen.Output_Directory:
			digest = o.Directory.TreeDigest.Hash
		case *gen.Output_DockerImage:
			digest = o.DockerImage.Digest
		default:
			return "", fmt.Errorf("unknown output type: %T", output.Kind)
		}
		_, err := hasher.WriteString(digest)
		if err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%x", hasher.Sum64()), nil
}

func sortOutputs(outputs []*gen.Output) {
	// Sort outputs by their path/tag to ensure consistent ordering
	sort.Slice(outputs, func(i, j int) bool {
		outputI := outputs[i]
		outputJ := outputs[j]

		return getOutputIdentifier(outputI) < getOutputIdentifier(outputJ)
	})
}

// getOutputIdentifier For each output get some identifier that we can use for sorting
func getOutputIdentifier(output *gen.Output) string {
	switch output.Kind.(type) {
	case *gen.Output_File:
		return output.GetFile().Path
	case *gen.Output_Directory:
		return output.GetDirectory().Path
	case *gen.Output_DockerImage:
		return output.GetDockerImage().Tag
	default:
		return ""
	}
}
