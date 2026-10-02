package loading

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestExcludingFSHidesExcludedDirectories(t *testing.T) {
	filesystem := excludingFS{
		FS: fstest.MapFS{
			"index.js":                &fstest.MapFile{},
			"node_modules/a/index.js": &fstest.MapFile{},
			"src/main.js":             &fstest.MapFile{},
		},
		excludeInputs: []string{"node_modules/**", "src/*"},
	}
	entries, err := filesystem.ReadDir(".")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"index.js", "src"}) {
		t.Errorf("Unexpected entries: %v", names)
	}
}
