package loading

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"grog/internal/config"
	"grog/internal/console"

	"github.com/apple/pkl-go/pkl"
)

const pklEvaluatorCreationTimeout = 2 * time.Minute

// PklLoader implements the Loader interface for pkl files.
type PklLoader struct {
	evaluator     pkl.Evaluator
	evaluatorErr  error
	evaluatorOnce sync.Once
}

func (pl *PklLoader) Matches(fileName string) bool {
	return fileName == "BUILD.pkl"
}

// getEvaluator lazily loads and caches the evaluator.
func (pl *PklLoader) getEvaluator(ctx context.Context) (pkl.Evaluator, error) {
	pl.evaluatorOnce.Do(func() {
		creationContext, cancel := newPklEvaluatorCreationContext(ctx)
		defer cancel()

		if hasPklProjectFile() {
			pl.evaluator, pl.evaluatorErr = pkl.NewProjectEvaluator(creationContext,
				&url.URL{Scheme: "file", Path: config.Global.WorkspaceRoot},
				pkl.PreconfiguredOptions,
				withEnv(LoaderEnv()),
				withEnv(config.Global.EnvironmentVariables),
			)
		} else {
			pl.evaluator, pl.evaluatorErr = pkl.NewEvaluator(creationContext,
				pkl.PreconfiguredOptions,
				withEnv(LoaderEnv()),
				withEnv(config.Global.EnvironmentVariables),
			)
		}

		if contextErr := creationContext.Err(); contextErr != nil && pl.evaluatorErr == nil {
			pl.evaluatorErr = fmt.Errorf("pkl evaluator creation did not complete within %s: %w", pklEvaluatorCreationTimeout, contextErr)
		}
	})
	return pl.evaluator, pl.evaluatorErr
}

func newPklEvaluatorCreationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), pklEvaluatorCreationTimeout)
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
		if errors.Is(err, context.DeadlineExceeded) {
			return pkg, false, err
		}
		return pkg, false, fmt.Errorf("found a BUILD.pkl file but the `pkl` cli is not available. " +
			"Please install it to use pkl files: https://pkl-lang.org/main/current/pkl-cli/index.html#installation")
	}
	if evaluator == nil {
		return pkg, false, errors.New("pkl evaluator creation returned no evaluator and no error")
	}

	evaluationTimeout := config.Global.GetPklEvaluationTimeout()
	evaluationContext, cancel := context.WithTimeout(ctx, evaluationTimeout)
	defer cancel()

	var evalErr error
	// pkl evaluator can panic so we need to be able to recover
	func() {
		defer func() {
			if r := recover(); r != nil {
				evalErr = fmt.Errorf("panic occurred while evaluating module: %v", r)
			}
		}()
		evalErr = evaluator.EvaluateModule(evaluationContext, pkl.FileSource(filePath), &pkg)
	}()

	// pkl-go v0.12.1 returns nil when its context is canceled, so checking only
	// evalErr could turn a timed-out module into a successful empty package.
	if contextErr := evaluationContext.Err(); contextErr != nil {
		return pkg, false, fmt.Errorf("pkl module evaluation for %q did not complete within %s: %w", filePath, evaluationTimeout, contextErr)
	}
	if evalErr != nil {
		return pkg, false, evalErr
	}

	return pkg, true, nil
}
