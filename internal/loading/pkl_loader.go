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
	"strings"
	"sync"

	"github.com/apple/pkl-go/pkl"
)

// PklLoader implements the Loader interface for pkl files.
type PklLoader struct {
	evaluator   pkl.Evaluator
	evaluatorMu sync.Mutex
}

func (pl *PklLoader) Matches(fileName string) bool {
	return "BUILD.pkl" == fileName
}

// getEvaluator lazily loads and caches the evaluator.
// Caller must hold evaluatorMu.
func (pl *PklLoader) getEvaluator(ctx context.Context) (pkl.Evaluator, error) {
	if pl.evaluator == nil {
		var evaluator pkl.Evaluator
		var err error

		if hasPklProjectFile() {
			projectURL, urlErr := pklFileURL(config.Global.WorkspaceRoot)
			if urlErr != nil {
				return nil, urlErr
			}
			evaluator, err = pkl.NewProjectEvaluator(ctx,
				projectURL,
				pkl.PreconfiguredOptions,
				withEnv(map[string]string{
					"GROG_OS":       config.Global.OS,
					"GROG_ARCH":     config.Global.Arch,
					"GROG_PLATFORM": config.Global.GetPlatform(),
				}),
				withEnv(config.Global.EnvironmentVariables),
			)
		} else {
			evaluator, err = pkl.NewEvaluator(ctx,
				pkl.PreconfiguredOptions,
				withEnv(map[string]string{
					"GROG_OS":       config.Global.OS,
					"GROG_ARCH":     config.Global.Arch,
					"GROG_PLATFORM": config.Global.GetPlatform(),
				}),
				withEnv(config.Global.EnvironmentVariables),
			)
		}
		if err != nil {
			return nil, err
		}
		pl.evaluator = evaluator
	}
	return pl.evaluator, nil
}

func hasPklProjectFile() bool {
	_, err := os.Stat(filepath.Join(config.Global.WorkspaceRoot, "PklProject"))
	return !errors.Is(err, os.ErrNotExist)
}

func pklFileURL(filePath string) (*url.URL, error) {
	absolutePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	slashPath := filepath.ToSlash(absolutePath)
	if filepath.VolumeName(absolutePath) != "" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return &url.URL{Scheme: "file", Path: slashPath}, nil
}

func pklFileSource(filePath string) (*pkl.ModuleSource, error) {
	fileURL, err := pklFileURL(filePath)
	if err != nil {
		return nil, err
	}
	return &pkl.ModuleSource{Uri: fileURL}, nil
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

	pl.evaluatorMu.Lock()
	defer pl.evaluatorMu.Unlock()

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
		source, sourceErr := pklFileSource(filePath)
		if sourceErr != nil {
			evalErr = sourceErr
			return
		}
		evalErr = evaluator.EvaluateModule(ctx, source, &pkg)
	}()

	if evalErr != nil {
		return pkg, false, evalErr
	}

	return pkg, true, nil
}
