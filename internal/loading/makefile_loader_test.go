package loading

import (
	"bufio"
	"strings"
	"testing"
)

// newScannerFromString creates a bufio.Scanner from a given string.
func newScannerFromString(s string) *bufio.Scanner {
	reader := strings.NewReader(s)
	return bufio.NewScanner(reader)
}

func TestMakefileParser(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedTargets   map[string]TargetDto
		expectedFoundFlag bool
		shouldError       bool
		errorContains     string
	}{
		{
			name: "Valid annotation and target",
			input: `# @grog
# name: grog_build_app
# deps:
#   - //proto:build
# inputs:
#   - src/**/*.js
#   - assets/**/*.scss
# outputs:
#   - dist/*.js
#   - dist/styles.bundle.css
build_app:
	npm run build`,
			expectedTargets: map[string]TargetDto{
				"grog_build_app": {
					Command: "make build_app",
					Deps:    []string{"//proto:build"},
					Inputs:  []string{"src/**/*.js", "assets/**/*.scss"},
					Outputs: []string{"dist/*.js", "dist/styles.bundle.css"},
				},
			},
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Valid annotation with no explicit 'name'",
			input: `# @grog
# deps:
#   - //proto:build
# inputs:
#   - src/**/*.js
# outputs:
#   - dist/*.js
build_target:
	echo "Building..."`,
			expectedTargets: map[string]TargetDto{
				"build_target": {
					Command: "make build_target",
					Deps:    []string{"//proto:build"},
					Inputs:  []string{"src/**/*.js"},
					Outputs: []string{"dist/*.js"},
				},
			},
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Missing annotation but with valid target definition",
			input: `build_app:
	npm run build`,
			// No annotation => no targets parsed by our loader.
			expectedTargets:   map[string]TargetDto{},
			expectedFoundFlag: false,
			shouldError:       false,
		},
		{
			name: "Annotation block with non matching target definition (missing colon)",
			input: `# @grog
# name: grog_bug
# deps:
#   - //proto:build
build_bug
	npm run build`,
			expectedTargets:   nil,
			expectedFoundFlag: true,
			shouldError:       true,
			errorContains:     "expected a make target definition",
		},
		{
			name: "Annotation block with YAML error in annotation",
			input: `# @grog
# name: grog_error: extra
# deps:
#   - //proto:build
build_error:
	echo "error"`,
			expectedTargets:   nil,
			expectedFoundFlag: true,
			shouldError:       true,
			errorContains:     "failed to parse annotation block",
		},
		{
			name: "Annotation block with no target definition following",
			input: `# @grog
# name: grog_empty
# deps:
#   - //proto:build`,
			expectedTargets:   map[string]TargetDto{}, // since no target definition was provided
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Multiple valid annotations in one file",
			input: `# @grog
# name: target_one
# deps:
#   - dep1
target_one:
	echo "target one"
# @grog
# name: target_two
# inputs:
#   - file1.js
target_two:
	echo "target two"`,
			expectedTargets: map[string]TargetDto{
				"target_one": {
					Command: "make target_one",
					Deps:    []string{"dep1"},
					Inputs:  nil,
					Outputs: nil,
				},
				"target_two": {
					Command: "make target_two",
					Deps:    nil,
					Inputs:  []string{"file1.js"},
					Outputs: nil,
				},
			},
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Annotation block with empty comment lines interleaved",
			input: `# @grog
#
# name: target_empty_lines
#
# deps:
#   - dep2
#
target_empty:
	echo "target with empty lines"`,
			expectedTargets: map[string]TargetDto{
				"target_empty_lines": {
					Command: "make target_empty",
					Deps:    []string{"dep2"},
					Inputs:  nil,
					Outputs: nil,
				},
			},
			expectedFoundFlag: true,
			shouldError:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new scanner from the input string.
			scanner := newScannerFromString(tc.input)
			parser := newMakefileParser(scanner)
			pkg, targetsFound, err := parser.parse()
			if tc.shouldError {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errorContains)
				}
				if !strings.Contains(err.Error(), tc.errorContains) {
					t.Fatalf("expected error containing %q, got: %v", tc.errorContains, err)
				}
				// In error scenarios, we don't further check pkg and targetsFound.
				return
			} else if err != nil {
				t.Fatalf("did not expect error, but got: %v", err)
			}

			// Verify the targets-found flag.
			if targetsFound != tc.expectedFoundFlag {
				t.Errorf("expected targetsFound flag to be %v, got %v", tc.expectedFoundFlag, targetsFound)
			}

			// Ensure the number of parsed targets match
			if len(pkg.Targets) != len(tc.expectedTargets) {
				t.Fatalf("expected %d targets, but got %d", len(tc.expectedTargets), len(pkg.Targets))
			}

			// Compare each target.
			for key, expected := range tc.expectedTargets {
				target, exists := pkg.Targets[key]
				if !exists {
					t.Errorf("expected target %q to be present", key)
					continue
				}
				if target.Command != expected.Command {
					t.Errorf("for target %q, expected command %q, got %q", key, expected.Command, target.Command)
				}
				compareStringSlices(t, expected.Deps, target.Deps, key, "Deps")
				compareStringSlices(t, expected.Inputs, target.Inputs, key, "Inputs")
				compareStringSlices(t, expected.Outputs, target.Outputs, key, "Outputs")
			}
		})
	}
}

// compareStringSlices compares two slices of strings.
func compareStringSlices(t *testing.T, expected, got []string, targetKey, field string) {
	if len(expected) != len(got) {
		t.Errorf("target %q: expected %s length %d; got %d", targetKey, field, len(expected), len(got))
		return
	}
	for i, exp := range expected {
		if exp != got[i] {
			t.Errorf("target %q: expected %s[%d] %q; got %q", targetKey, field, i, exp, got[i])
		}
	}
}
