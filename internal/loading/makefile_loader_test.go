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
		expectedTargets   []TargetDTO
		expectedFoundFlag bool
		shouldError       bool
		errorContains     string
	}{
		{
			name: "Valid annotation and target",
			input: `# @grog
# name: grog_build_app
# dependencies:
#   - //proto:build
# inputs:
#   - src/**/*.js
#   - assets/**/*.scss
# outputs:
#   - dist/*.js
#   - dist/styles.bundle.css
build_app:
	npm run build`,
			expectedTargets: []TargetDTO{
				{
					Name:         "grog_build_app",
					Command:      "make build_app",
					Dependencies: []string{"//proto:build"},
					Inputs:       []string{"src/**/*.js", "assets/**/*.scss"},
					Outputs:      []string{"dist/*.js", "dist/styles.bundle.css"},
				},
			},
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Valid annotation with no explicit 'name'",
			input: `# @grog
# dependencies:
#   - //proto:build
# inputs:
#   - src/**/*.js
# outputs:
#   - dist/*.js
build_target:
	echo "Building..."`,
			expectedTargets: []TargetDTO{
				{
					Name:         "build_target",
					Command:      "make build_target",
					Dependencies: []string{"//proto:build"},
					Inputs:       []string{"src/**/*.js"},
					Outputs:      []string{"dist/*.js"},
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
			expectedTargets:   []TargetDTO{},
			expectedFoundFlag: false,
			shouldError:       false,
		},
		{
			name: "Annotation block with non matching target definition (missing colon)",
			input: `# @grog
# name: grog_bug
# dependencies:
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
# dependencies:
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
# dependencies:
#   - //proto:build`,
			expectedTargets:   []TargetDTO{}, // since no target definition was provided
			expectedFoundFlag: true,
			shouldError:       false,
		},
		{
			name: "Multiple valid annotations in one file",
			input: `# @grog
# name: target_one
# dependencies:
#   - dep1
target_one:
	echo "target one"
# @grog
# name: target_two
# inputs:
#   - file1.js
target_two:
	echo "target two"`,
			expectedTargets: []TargetDTO{
				{
					Name:         "target_one",
					Command:      "make target_one",
					Dependencies: []string{"dep1"},
					Inputs:       nil,
					Outputs:      nil,
				},
				{
					Name:         "target_two",
					Command:      "make target_two",
					Dependencies: nil,
					Inputs:       []string{"file1.js"},
					Outputs:      nil,
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
# dependencies:
#   - dep2
#
target_empty:
	echo "target with empty lines"`,
			expectedTargets: []TargetDTO{
				{
					Name:         "target_empty_lines",
					Command:      "make target_empty",
					Dependencies: []string{"dep2"},
					Inputs:       nil,
					Outputs:      nil,
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
			for index, expected := range tc.expectedTargets {
				target := pkg.Targets[index]
				if target.Command != expected.Command {
					t.Errorf("for target %d, expected command %q, got %q", index, expected.Command, target.Command)
				}
				compareStringSlices(t, expected.Dependencies, target.Dependencies, index, "Dependencies")
				compareStringSlices(t, expected.Inputs, target.Inputs, index, "Inputs")
				compareStringSlices(t, expected.Outputs, target.Outputs, index, "Outputs")
			}
		})
	}
}

// compareStringSlices compares two slices of strings.
func compareStringSlices(t *testing.T, expected, got []string, targetIndex int, field string) {
	if len(expected) != len(got) {
		t.Errorf("target %d: expected %s length %d; got %d", targetIndex, field, len(expected), len(got))
		return
	}
	for i, exp := range expected {
		if exp != got[i] {
			t.Errorf("target %d: expected %s[%d] %q; got %q", targetIndex, field, i, exp, got[i])
		}
	}
}
