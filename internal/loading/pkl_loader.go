package loading

import (
	"context"
	"errors"
	"fmt"
	"grog/internal/config"
	"grog/internal/console"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/apple/pkl-go/pkl"
)

// PklLoader implements the Loader interface for pkl files.
type PklLoader struct {
	evaluator     pkl.Evaluator
	evaluatorErr  error
	evaluatorOnce sync.Once
}

func (pl *PklLoader) Matches(fileName string) bool {
	return "BUILD.pkl" == fileName
}

// getEvaluator lazily loads and caches the evaluator.
func (pl *PklLoader) getEvaluator(ctx context.Context) (pkl.Evaluator, error) {
	pl.evaluatorOnce.Do(func() {
		if hasPklProjectFile() {
			pl.evaluator, pl.evaluatorErr = pkl.NewProjectEvaluator(ctx,
				&url.URL{Scheme: "file", Path: config.Global.WorkspaceRoot},
				pkl.PreconfiguredOptions,
				withEnv(map[string]string{
					"GROG_OS":       config.Global.OS,
					"GROG_ARCH":     config.Global.Arch,
					"GROG_PLATFORM": config.Global.GetPlatform(),
				}),
				withEnv(config.Global.EnvironmentVariables),
			)
		} else {
			pl.evaluator, pl.evaluatorErr = pkl.NewEvaluator(ctx,
				pkl.PreconfiguredOptions,
				withEnv(map[string]string{
					"GROG_OS":       config.Global.OS,
					"GROG_ARCH":     config.Global.Arch,
					"GROG_PLATFORM": config.Global.GetPlatform(),
				}),
				withEnv(config.Global.EnvironmentVariables),
			)
		}
	})
	return pl.evaluator, pl.evaluatorErr
}

func hasPklProjectFile() bool {
	_, err := os.Stat(filepath.Join(config.Global.WorkspaceRoot, "PklProject"))
	return !errors.Is(err, os.ErrNotExist)
}

// withEnv adds or overrides environment variables for the `env:` resource reader.
// Any key in envVars will be set into EvaluatorOptions.Env.
func withEnv(envVars map[string]string) func(*pkl.EvaluatorOptions) {
	return func(opts *pkl.EvaluatorOptions) {
		if opts.Env == nil {
			opts.Env = make(map[string]string, len(envVars))
		}
		maps.Copy(opts.Env, envVars)
	}
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (pl *PklLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	var pkg PackageDTO

	evaluator, err := pl.getEvaluator(ctx)
	if err != nil {
		console.GetLogger(ctx).Debugf("failed to get evaluator: %v", err)
		return pkg, false, fmt.Errorf("found a BUILD.pkl file but the `pkl` cli is not available. " +
			"Please install it to use pkl files: https://pkl-lang.org/main/current/pkl-cli/index.html#installation")
	}

	var evalErr error
	// pkl evaluator can panic so we need to be able to recover
	func() {
		defer func() {
			if r := recover(); r != nil {
				evalErr = fmt.Errorf("panic occurred while evaluating module: %v", r)
			}
		}()
		evalErr = evaluator.EvaluateModule(ctx, pkl.FileSource(filePath), &pkg)
	}()

	if evalErr != nil {
		return pkg, false, evalErr
	}

	return pkg, true, nil
}
