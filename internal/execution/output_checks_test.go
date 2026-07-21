package execution

import (
	"context"
	"testing"

	"grog/internal/label"
	"grog/internal/model"
)

func TestRunOutputChecksReceivesResourceEnvironment(t *testing.T) {
	setupResourceTestWorkspace(t)
	target := &model.Target{
		Label: label.TargetLabel{Package: "pkg", Name: "consumer"},
		OutputChecks: []model.OutputCheck{
			{Command: `test "$RESOURCE_TOKEN" = resource-value`},
		},
	}

	err := runOutputChecks(context.Background(), target, nil, nil, []string{"RESOURCE_TOKEN=resource-value"})
	if err != nil {
		t.Fatalf("output check did not receive resource environment: %v", err)
	}
}
