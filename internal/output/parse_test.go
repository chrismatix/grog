package output

import (
	"grog/internal/model"
	"grog/internal/output/handlers"
	"testing"
)

func TestParseOutput(t *testing.T) {
	tests := []struct {
		input       string
		expected    model.Output
		expectError bool
	}{
		{
			input:    "dist/main",
			expected: model.NewOutput(string(handlers.FileHandler), "dist/main"),
		},
		{
			input:    "dir::dist/",
			expected: model.NewOutput("dir", "dist/"),
		},
		{
			input:       "unknown::foo",
			expectError: true,
		},
		{
			input:       "incomplete::",
			expectError: true,
		},
		{
			input:       "malformed::extra::colon",
			expectError: true, // SplitN ensures only 2 parts, so only the first "::" matters
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			out, err := ParseOutput(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error for input %q but got none", tt.input)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for input %q: %v", tt.input, err)
				}
				if out.Type != tt.expected.Type || out.Identifier != tt.expected.Identifier {
					t.Errorf("unexpected output for %q: got %+v, want %+v", tt.input, out, tt.expected)
				}
			}
		})
	}
}

func TestParseOutputs(t *testing.T) {
	valid := []string{"dist/main", "dir::dist/", "file::output.txt"}
	expected := []model.Output{
		model.NewOutput("file", "dist/main"),
		model.NewOutput("dir", "dist/"),
		model.NewOutput("file", "output.txt"),
	}

	out, err := ParseOutputs(valid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != len(expected) {
		t.Fatalf("expected %d outputs, got %d", len(expected), len(out))
	}
	for i := range expected {
		if out[i] != expected[i] {
			t.Errorf("output[%d] = %+v; want %+v", i, out[i], expected[i])
		}
	}

	invalid := []string{"dist/main", "badtype::xxx"}
	_, err = ParseOutputs(invalid)
	if err == nil {
		t.Fatalf("expected error for invalid outputs but got none")
	}
}
