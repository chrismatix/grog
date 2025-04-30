package loading

import (
	"context"
	"fmt"
	"github.com/apple/pkl-go/pkl"
	"grog/internal/console"
)

// PklLoader implements the Loader interface for pkl files.
type PklLoader struct {
	evaluator pkl.Evaluator
}

func (pl PklLoader) FileNames() []string {
	return []string{"BUILD.pkl"}
}

// getEvaluator lazily loads and caches the evaluator
func (pl PklLoader) getEvaluator() (pkl.Evaluator, error) {
	if pl.evaluator == nil {
		evaluator, err := pkl.NewEvaluator(context.Background(), pkl.PreconfiguredOptions)
		if err != nil {
			return nil, err
		}
		pl.evaluator = evaluator
	}
	return pl.evaluator, nil
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (pl PklLoader) Load(ctx context.Context, filePath string) (PackageDTO, bool, error) {
	var pkg PackageDTO

	evaluator, err := pl.getEvaluator()
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
