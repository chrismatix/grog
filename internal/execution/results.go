package execution

import (
	"cmp"
	"slices"

	"github.com/chrismatix/grog/internal/dag"
	"github.com/chrismatix/grog/internal/model"
	"github.com/chrismatix/grog/internal/output/handlers"
	"github.com/chrismatix/grog/internal/proto/gen"
)

// TargetBuildResult is one target's entry in the `grog build --results` file.
type TargetBuildResult struct {
	ChangeHash string         `json:"change_hash"`
	OutputHash string         `json:"output_hash"`
	CacheHit   bool           `json:"cache_hit"`
	Outputs    []OutputResult `json:"outputs"`
	Pushed     []PushResult   `json:"pushed"`
}

// OutputResult identifies the content of a single target output.
type OutputResult struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// PushResult is the manifest digest an oci_push destination serves after the build.
type PushResult struct {
	Destination string `json:"destination"`
	Digest      string `json:"digest"`
}

// Results reports the hashes, output digests and pushes of every target in the
// completion map, keyed by label. Pushes run as async cache writes, so call it
// only after WaitForAsyncWrites, and only when none of them failed.
func (e *Executor) Results(completionMap dag.CompletionMap) map[string]TargetBuildResult {
	pushedByLabel := make(map[string][]PushResult)
	for _, report := range e.pusher.Reporter().Reports() {
		pushedByLabel[report.TargetLabel] = append(pushedByLabel[report.TargetLabel], PushResult{
			Destination: report.Destination,
			Digest:      report.Digest,
		})
	}

	results := make(map[string]TargetBuildResult)
	for targetLabel, completion := range completionMap {
		target, isTarget := e.graph.GetNodes()[targetLabel].(*model.Target)
		if !isTarget {
			continue
		}

		// Empty lists rather than null, so consumers can iterate without a check.
		outputs := []OutputResult{}
		if targetResult, ok := e.targetResults.Load(targetLabel); ok {
			for _, targetOutput := range targetResult.(*gen.TargetResult).GetOutputs() {
				switch targetOutput.GetKind().(type) {
				case *gen.Output_File:
					outputs = append(outputs, OutputResult{
						Kind:   string(handlers.FileHandler),
						Name:   targetOutput.GetFile().GetPath(),
						Digest: targetOutput.GetFile().GetDigest().GetHash(),
					})
				case *gen.Output_Directory:
					outputs = append(outputs, OutputResult{
						Kind:   string(handlers.DirHandler),
						Name:   targetOutput.GetDirectory().GetPath(),
						Digest: targetOutput.GetDirectory().GetTreeDigest().GetHash(),
					})
				case *gen.Output_OciImage:
					outputs = append(outputs, OutputResult{
						Kind:   string(handlers.OCIHandler),
						Name:   targetOutput.GetOciImage().GetLocalTag(),
						Digest: targetOutput.GetOciImage().GetImageId(),
					})
				}
			}
		}
		// Outputs are written in parallel, so their cached order is arbitrary.
		slices.SortFunc(outputs, func(left, right OutputResult) int {
			return cmp.Or(cmp.Compare(left.Kind, right.Kind), cmp.Compare(left.Name, right.Name))
		})

		results[targetLabel.String()] = TargetBuildResult{
			ChangeHash: target.ChangeHash,
			OutputHash: target.OutputHash,
			CacheHit:   completion.CacheResult == dag.CacheHit,
			Outputs:    outputs,
			Pushed:     append([]PushResult{}, pushedByLabel[targetLabel.String()]...),
		}
	}
	return results
}
