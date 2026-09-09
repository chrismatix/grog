package loading

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/label"
	"grog/internal/model"
	"grog/internal/shell"

	"golang.org/x/sync/errgroup"
)

type providerDocument struct {
	Version  int                        `json:"version"`
	Packages map[string]providerPackage `json:"packages"`
}

type providerPackage struct {
	Dependencies []string `json:"dependencies"`
}

func inferDependencies(loadContext context.Context, packages []*model.Package) error {
	declarations := make(map[label.TargetLabel]*model.DependencyProvider)
	registrations := make(map[label.TargetLabel]map[string]*model.Target)
	var registeringTargets []*model.Target
	for _, loadedPackage := range packages {
		maps.Copy(declarations, loadedPackage.DependencyProviders)
		for _, target := range loadedPackage.Targets {
			if len(target.DependencyProviders) > 0 {
				registeringTargets = append(registeringTargets, target)
			}
		}
	}
	declaredLabels := make([]string, 0, len(declarations))
	for providerLabel := range declarations {
		declaredLabels = append(declaredLabels, providerLabel.String())
	}
	slices.Sort(declaredLabels)
	slices.SortFunc(registeringTargets, func(first, second *model.Target) int {
		return strings.Compare(first.Label.String(), second.Label.String())
	})
	for _, target := range registeringTargets {
		targetLabel := target.Label
		for _, providerLabel := range target.DependencyProviders {
			if declarations[providerLabel] == nil {
				return fmt.Errorf("no dependency provider at %s (referenced by %s); declared: %s", providerLabel, targetLabel, strings.Join(declaredLabels, ", "))
			}
			if registrations[providerLabel] == nil {
				registrations[providerLabel] = make(map[string]*model.Target)
			}
			if previous := registrations[providerLabel][path.Join(targetLabel.Package)]; previous != nil && previous.Label != targetLabel {
				return fmt.Errorf("provider %s registered twice in package %s: %s and %s", providerLabel, targetLabel.Package, previous.Label, targetLabel)
			}
			registrations[providerLabel][path.Join(targetLabel.Package)] = target
		}
	}
	providerLabels := slices.Collect(maps.Keys(registrations))
	slices.SortFunc(providerLabels, func(first, second label.TargetLabel) int { return strings.Compare(first.String(), second.String()) })
	documents := make([]providerDocument, len(providerLabels))
	providerGroup, providerContext := errgroup.WithContext(loadContext)
	for index, providerLabel := range providerLabels {
		providerGroup.Go(func() error {
			document, err := runDependencyProvider(providerContext, declarations[providerLabel])
			if err != nil {
				return err
			}
			documents[index] = document
			return nil
		})
	}
	if err := providerGroup.Wait(); err != nil {
		return err
	}
	for index, providerLabel := range providerLabels {
		document := documents[index]
		for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
			target := registrations[providerLabel][path.Join(providerLabel.Package, packagePath)]
			if target == nil {
				console.GetLogger(loadContext).Debugf("provider %s reports unregistered package %s; ignoring", providerLabel, packagePath)
				continue
			}
			for _, dependency := range document.Packages[packagePath].Dependencies {
				var dependencyLabel label.TargetLabel
				if strings.HasPrefix(dependency, "//") {
					parsedLabel, err := label.ParseTargetLabel("", dependency)
					if err != nil {
						return fmt.Errorf("provider %s reports invalid dependency %q: %w", providerLabel, dependency, err)
					}
					dependencyLabel = parsedLabel
				} else {
					dependencyTarget := registrations[providerLabel][path.Join(providerLabel.Package, dependency)]
					if dependencyTarget == nil {
						return fmt.Errorf("provider %s reports %s depends on %s, which has no target registered for %s", providerLabel, packagePath, dependency, providerLabel)
					}
					dependencyLabel = dependencyTarget.Label
				}
				if dependencyLabel != target.Label {
					target.Dependencies = append(target.Dependencies, dependencyLabel)
				}
			}
		}
	}
	for _, providerTargets := range registrations {
		for _, target := range providerTargets {
			slices.SortFunc(target.Dependencies, func(first, second label.TargetLabel) int { return strings.Compare(first.String(), second.String()) })
			target.Dependencies = slices.Compact(target.Dependencies)
		}
	}
	return loadContext.Err()
}

func runDependencyProvider(loadContext context.Context, provider *model.DependencyProvider) (providerDocument, error) {
	providerContext, cancel := context.WithTimeout(loadContext, provider.Timeout)
	defer cancel()
	logger := console.GetLogger(loadContext)
	logger.Debugf("provider %s: %s", provider.Label, provider.Command)
	var document providerDocument
	if provider.Command == "builtin:cargo" {
		var err error
		document, err = cargoDependencies(providerContext, config.GetPathAbsoluteToWorkspaceRoot(provider.Label.Package))
		if err != nil {
			return document, fmt.Errorf("provider %s failed: %w", provider.Label, err)
		}
	} else {
		command, cleanup, err := shell.NewCommand(providerContext, shell.WithDefaultFlags(provider.Command))
		if err != nil {
			return document, fmt.Errorf("provider %s failed: %w", provider.Label, err)
		}
		defer cleanup()
		command.Dir = config.GetPathAbsoluteToWorkspaceRoot(provider.Label.Package)
		command.Env = os.Environ()
		for name, value := range config.Global.EnvironmentVariables {
			command.Env = append(command.Env, name+"="+value)
		}
		for name, value := range LoaderEnv() {
			command.Env = append(command.Env, name+"="+value)
		}
		command.Env = append(command.Env, "GROG_PROVIDER_LABEL="+provider.Label.String(), "GROG_TARGET="+provider.Label.String(), "GROG_PACKAGE="+provider.Label.Package)
		var standardOutput, standardError bytes.Buffer
		command.Stdout = &standardOutput
		command.Stderr = &standardError
		if err := command.Run(); err != nil {
			if providerContext.Err() != nil {
				return document, fmt.Errorf("provider %s failed: timeout or cancellation after %s: %w\n%s", provider.Label, provider.Timeout, providerContext.Err(), standardError.String())
			}
			return document, fmt.Errorf("provider %s failed: %w\n%s", provider.Label, err, standardError.String())
		}
		logger.Debugf("provider %s stderr: %s", provider.Label, standardError.String())
		if err := json.Unmarshal(standardOutput.Bytes(), &document); err != nil {
			return document, fmt.Errorf("provider %s returned invalid JSON: %w; stdout: %q", provider.Label, err, standardOutput.Bytes()[:min(standardOutput.Len(), 2048)])
		}
	}
	if document.Version > 1 {
		return document, fmt.Errorf("provider %s returned unsupported version %d; upgrade grog", provider.Label, document.Version)
	}
	if document.Version != 1 {
		return document, fmt.Errorf("provider %s must return version 1", provider.Label)
	}
	if document.Packages == nil {
		return document, fmt.Errorf("provider %s must return a packages object", provider.Label)
	}
	for _, packagePath := range slices.Sorted(maps.Keys(document.Packages)) {
		if err := validateProviderPath(packagePath); err != nil {
			return document, fmt.Errorf("provider %s: %w", provider.Label, err)
		}
		reportedPackage := document.Packages[packagePath]
		slices.Sort(reportedPackage.Dependencies)
		for _, dependency := range reportedPackage.Dependencies {
			if strings.HasPrefix(dependency, "//") {
				continue
			}
			if err := validateProviderPath(dependency); err != nil {
				return document, fmt.Errorf("provider %s: %w", provider.Label, err)
			}
		}
	}
	logger.Debugf("provider %s mapping: %+v", provider.Label, document)
	return document, nil
}

func validateProviderPath(entry string) error {
	if strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "./") || strings.HasSuffix(entry, "/") || strings.Contains(entry, "\\") || (len(entry) > 1 && entry[1] == ':') || slices.Contains(strings.Split(entry, "/"), "..") {
		return fmt.Errorf("invalid path %q: paths must be relative, slash-separated, without a leading ./, trailing /, or .. segment", entry)
	}
	return nil
}
