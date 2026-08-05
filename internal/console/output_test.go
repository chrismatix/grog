package console

import (
	"errors"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigureJSONFromArgumentsStopsAtDelimiter(t *testing.T) {
	testCases := []struct {
		name      string
		arguments []string
		expected  bool
	}{
		{
			name:      "grog flag",
			arguments: []string{"run", "--json", "//:tool", "--", "input"},
			expected:  true,
		},
		{
			name:      "child flag",
			arguments: []string{"run", "//:tool", "--", "--json"},
			expected:  false,
		},
		{
			name:      "child true flag",
			arguments: []string{"run", "//:tool", "--", "--json=true"},
			expected:  false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			viper.Set("json", false)
			if actual := ConfigureJSONFromArguments(testCase.arguments); actual != testCase.expected {
				t.Errorf("expected %t, got %t", testCase.expected, actual)
			}
			if JSONEnabled() != testCase.expected {
				t.Errorf("expected JSON enabled to be %t", testCase.expected)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		expectedCode   string
		expectedStatus int
	}{
		{
			name:           "cobra errors default to invocation",
			err:            errors.New("unknown flag"),
			expectedCode:   ErrorCodeInvalidInvocation,
			expectedStatus: 2,
		},
		{
			name:           "runtime failure",
			err:            RuntimeFailure(errors.New("invalid config")),
			expectedCode:   ErrorCodeRuntimeFailure,
			expectedStatus: 1,
		},
		{
			name:           "existing classification is preserved",
			err:            RuntimeFailure(InvalidInvocation(errors.New("invalid platform"))),
			expectedCode:   ErrorCodeInvalidInvocation,
			expectedStatus: 2,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actualCode, actualStatus := ClassifyError(testCase.err)
			if actualCode != testCase.expectedCode || actualStatus != testCase.expectedStatus {
				t.Errorf("expected %s/%d, got %s/%d", testCase.expectedCode, testCase.expectedStatus, actualCode, actualStatus)
			}
		})
	}
}
