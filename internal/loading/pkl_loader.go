package loading

import (
	"context"
	"fmt"
	"github.com/apple/pkl-go/pkl"
	"grog/internal/console"
)

type PklTargetDto struct {
	Name    string   `pkl:"name"`
	Command string   `pkl:"cmd"`
	Deps    []string `pkl:"deps"`
	Inputs  []string `pkl:"inputs"`
	Outputs []string `pkl:"outputs"`
}

type PklPackageDto struct {
	Targets []PklTargetDto `pkl:"targets"`
}

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
			// check if the pkl exe is available and create a special error if not

			return nil, err
		}
		pl.evaluator = evaluator
	}
	return pl.evaluator, nil
}

// Load reads the file at the specified filePath and unmarshals its content into a model.Package.
func (pl PklLoader) Load(ctx context.Context, filePath string) (PackageDto, bool, error) {
	var pklPackage PklPackageDto
	var pkg PackageDto

	evaluator, err := pl.getEvaluator()
	if err != nil {
		console.GetLogger(ctx).Debugf("failed to get evaluator: %v", err)
		return pkg, false, fmt.Errorf("found a BUILD.pkl file but the `pkl` cli is not available. " +
			"Please install it to use pkl files: https://pkl-lang.org/main/current/pkl-cli/index.html#installation")
	}
	if err = evaluator.EvaluateModule(ctx, pkl.FileSource(filePath), &pklPackage); err != nil {
		return pkg, false, err
	}

	pkg.Targets = make(map[string]*TargetDto)
	for _, targetDto := range pklPackage.Targets {
		mappedTarget := &TargetDto{
			Deps:    targetDto.Deps,
			Command: targetDto.Command,
			Outputs: targetDto.Outputs,
			Inputs:  targetDto.Inputs,
		}
		pkg.Targets[targetDto.Name] = mappedTarget
	}

	return pkg, true, nil
}
