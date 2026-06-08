package loading

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestJsonLoader_OciPush covers the scalar-or-list value normalisation on
// the JSON path. The DTO's custom UnmarshalJSON is the only place this is
// exercised — everything downstream sees a uniform []string.
func TestJsonLoader_OciPush(t *testing.T) {
	tmpDir := t.TempDir()
	build := filepath.Join(tmpDir, "BUILD.json")
	if err := os.WriteFile(build, []byte(`{
  "targets": [
    {
      "name": "app",
      "command": "docker build -t app .",
      "outputs": ["oci::app"],
      "oci_push": {
        "app": "registry.org/app:1.0.0",
        "worker": ["registry.org/worker:1.0.0", "registry.org/worker:latest"]
      }
    }
  ]
}`), 0644); err != nil {
		t.Fatal(err)
	}

	pkg, _, err := (JsonLoader{}).Load(context.Background(), build)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(pkg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(pkg.Targets))
	}
	push := pkg.Targets[0].OciPush
	if got, want := push["app"], (ociPushDestinations{"registry.org/app:1.0.0"}); !reflect.DeepEqual(got, want) {
		t.Errorf("scalar destination normalised wrong: %v", got)
	}
	if got, want := push["worker"], (ociPushDestinations{"registry.org/worker:1.0.0", "registry.org/worker:latest"}); !reflect.DeepEqual(got, want) {
		t.Errorf("list destinations preserved wrong: %v, want %v", got, want)
	}
}
