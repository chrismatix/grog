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
	"runtime"
	"sync"

	"github.com/apple/pkl-go/pkl"
)

// PklLoader implements the Loader interface for pkl files.
type PklLoader struct {
	evaluator       pkl.Evaluator
	evaluatorErr    error
	evaluatorOnce   sync.Once
	evaluationMutex sync.Mutex
}

func (pl *PklLoader) Matches(fileName string) bool {
	return fileName == "BUILD.pkl"
}

// getEvaluator lazily loads and caches the evaluator.
func (pl *PklLoader) getEvaluator(ctx context.Context) (pkl.Evaluator, error) {
	pl.evaluatorOnce.Do(func() {
		if hasPklProjectFile() {
			pl.evaluator, pl.evaluatorErr = pkl.NewProjectEvaluator(ctx,
				pklFileSource(config.Global.WorkspaceRoot).Uri,
				pkl.PreconfiguredOptions,
				withEnv(LoaderEnv()),
				withEnv(config.Global.EnvironmentVariables),
			)
		} else {
			pl.evaluator, pl.evaluatorErr = pkl.NewEvaluator(ctx,
				pkl.PreconfiguredOptions,
				withEnv(LoaderEnv()),
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
	// pkl-go's clock-seeded RNGs can generate duplicate concurrent request IDs on Windows.
	if runtime.GOOS == "windows" {
		pl.evaluationMutex.Lock()
		defer pl.evaluationMutex.Unlock()
	}
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
		evalErr = evaluator.EvaluateModule(ctx, pklFileSource(filePath), &pkg)
	}()

	if evalErr != nil {
		return pkg, false, evalErr
	}

	return pkg, true, nil
}

func pklFileSource(filePath string) *pkl.ModuleSource {
	if runtime.GOOS != "windows" {
		return pkl.FileSource(filePath)
	}
	return &pkl.ModuleSource{Uri: &url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(filePath)}}
}
