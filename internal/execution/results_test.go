package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chrismatix/grog/internal/caching"
	"github.com/chrismatix/grog/internal/caching/backends"
	"github.com/chrismatix/grog/internal/config"
	"github.com/chrismatix/grog/internal/dag"
	"github.com/chrismatix/grog/internal/label"
	"github.com/chrismatix/grog/internal/model"
	"github.com/chrismatix/grog/internal/output"
	"github.com/chrismatix/grog/internal/output/handlers"
	"github.com/chrismatix/grog/internal/output/push"
	"github.com/chrismatix/grog/internal/proto/gen"

	"github.com/stretchr/testify/require"
)

// A release pipeline diffs the results of two builds, so a full cache hit must
// report exactly what the fresh build of the same sources reported.
func TestResultsMatchBetweenFreshBuildAndFullCacheHit(t *testing.T) {
	testCases := []struct {
		name                  string
		cachedLoadOutputsMode config.LoadOutputsMode
	}{
		{name: "cached build loads all outputs", cachedLoadOutputsMode: config.LoadOutputsAll},
		{name: "cached build loads minimal outputs", cachedLoadOutputsMode: config.LoadOutputsMinimal},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			previousConfig := config.Global
			config.Global = config.WorkspaceConfig{
				WorkspaceRoot: workspaceRoot,
				Root:          t.TempDir(),
				NumWorkers:    2,
				NumIOWorkers:  1,
				EnableCache:   true,
			}
			t.Cleanup(func() { config.Global = previousConfig })

			require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "input.txt"), []byte("hello\n"), 0644))
			cacheBackend := backends.NewFileSystemCacheForTest(t.TempDir(), t.TempDir())

			build := func(loadOutputsMode config.LoadOutputsMode) map[string]TargetBuildResult {
				producer := &model.Target{
					Label:      label.TL("", "producer"),
					Inputs:     []string{"input.txt"},
					Outputs:    []model.Output{model.NewOutput("file", "output.txt"), model.NewOutput("dir", "dist")},
					Command:    "cp input.txt output.txt && mkdir -p dist && cp input.txt dist/copy.txt",
					IsSelected: true,
				}
				consumer := &model.Target{
					Label:        label.TL("", "consumer"),
					Dependencies: []label.TargetLabel{producer.Label},
					Outputs:      []model.Output{model.NewOutput("file", "consumed.txt")},
					Command:      "cp output.txt consumed.txt",
					IsSelected:   true,
				}
				aggregator := &model.Target{
					Label:        label.TL("", "aggregator"),
					Dependencies: []label.TargetLabel{consumer.Label},
					IsSelected:   true,
				}
				graph := dag.NewDirectedGraphFromTargets(producer, consumer, aggregator)
				require.NoError(t, graph.AddEdge(producer, consumer))
				require.NoError(t, graph.AddEdge(consumer, aggregator))

				ctx := context.Background()
				executor := NewExecutor(
					caching.NewTargetResultCache(cacheBackend),
					caching.NewTaintStore(),
					output.NewRegistry(ctx, caching.NewCas(cacheBackend)),
					nil,
					graph,
					true,
					false,
					true,
					loadOutputsMode,
				)
				completionMap, err := executor.Execute(ctx)
				require.NoError(t, err)
				require.Empty(t, completionMap.GetErrors())
				return executor.Results(completionMap)
			}

			freshResults := build(config.LoadOutputsAll)
			require.Len(t, freshResults, 3)
			require.Len(t, freshResults["//:producer"].Outputs, 2)
			require.Len(t, freshResults["//:consumer"].Outputs, 1)
			for targetLabel, freshResult := range freshResults {
				require.False(t, freshResult.CacheHit, targetLabel)
				require.NotEmpty(t, freshResult.ChangeHash, targetLabel)
				require.NotEmpty(t, freshResult.OutputHash, targetLabel)
			}

			cachedResults := build(testCase.cachedLoadOutputsMode)
			for targetLabel, cachedResult := range cachedResults {
				require.True(t, cachedResult.CacheHit, targetLabel)
				cachedResult.CacheHit = false
				cachedResults[targetLabel] = cachedResult
			}
			require.Equal(t, freshResults, cachedResults)
		})
	}
}

func TestResultsReportOciImageIDsAndSuccessfulPushes(t *testing.T) {
	target := &model.Target{
		Label:      label.TL("services/api", "image"),
		ChangeHash: "change-hash",
		OutputHash: "output-hash",
	}
	alias := &model.Alias{Label: label.TL("services/api", "alias")}

	pusher, err := push.New(handlers.NewDockerOutputHandler(context.Background(), nil, nil), nil, nil)
	require.NoError(t, err)
	for _, report := range []handlers.PushReport{
		{TargetLabel: "//services/api:image", Destination: "registry/api:abc123", Digest: "sha256:manifest"},
		{TargetLabel: "//services/api:image", Destination: "registry/api:latest", Digest: "sha256:manifest", Skipped: true},
		{TargetLabel: "//services/api:image", Destination: "registry/api:broken", Err: errors.New("503")},
	} {
		pusher.Reporter().Record(report)
	}

	executor := &Executor{graph: dag.NewDirectedGraphFromTargets(target, alias), pusher: pusher}
	executor.targetResults.Store(target.Label, &gen.TargetResult{
		Outputs: []*gen.Output{{Kind: &gen.Output_OciImage{OciImage: &gen.OCIImageOutput{
			LocalTag:       "api",
			ImageId:        "sha256:config",
			ManifestDigest: &gen.Digest{Hash: "sha256:manifest"},
		}}}},
	})

	results := executor.Results(dag.CompletionMap{
		target.Label: {IsSuccess: true, NodeType: model.TargetNode, CacheResult: dag.CacheHit},
		alias.Label:  {IsSuccess: true, NodeType: model.AliasNode, CacheResult: dag.CacheHit},
	})

	require.Equal(t, map[string]TargetBuildResult{
		"//services/api:image": {
			ChangeHash: "change-hash",
			OutputHash: "output-hash",
			CacheHit:   true,
			Outputs:    []OutputResult{{Kind: "oci", Name: "api", Digest: "sha256:config"}},
			Pushed: []PushResult{
				{Destination: "registry/api:abc123", Digest: "sha256:manifest"},
				{Destination: "registry/api:latest", Digest: "sha256:manifest"},
			},
		},
	}, results)
}
