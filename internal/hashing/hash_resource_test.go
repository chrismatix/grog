package hashing

import (
	"testing"
	"time"

	"grog/internal/label"
	"grog/internal/model"
)

func TestGetResourceIdentityIncludesCompleteDefinition(t *testing.T) {
	base := model.Resource{
		Label:   label.TL("pkg", "database"),
		Up:      "start",
		Down:    "stop",
		Ready:   "ready",
		Exports: map[string]string{"DATABASE_URL": "postgres://local"},
	}
	baseIdentity := GetResourceIdentity(base)

	testCases := []struct {
		name     string
		resource model.Resource
	}{
		{
			name: "timeout",
			resource: func() model.Resource {
				modified := base
				modified.Timeout = time.Minute
				return modified
			}(),
		},
		{
			name: "dependencies",
			resource: func() model.Resource {
				modified := base
				modified.Dependencies = []label.TargetLabel{label.TL("pkg", "network")}
				return modified
			}(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if identity := GetResourceIdentity(testCase.resource); identity == baseIdentity {
				t.Fatalf("resource identity did not change for %s", testCase.name)
			}
		})
	}
}

func TestGetResourceIdentitySeparatesFieldBoundaries(t *testing.T) {
	first := model.Resource{Label: label.TL("pkg", "database"), Up: "ab", Down: "c"}
	second := model.Resource{Label: first.Label, Up: "a", Down: "bc"}

	if GetResourceIdentity(first) == GetResourceIdentity(second) {
		t.Fatal("resource identities collided across field boundaries")
	}
}
