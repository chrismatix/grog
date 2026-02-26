package backends

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestFileSystemCache_SetGetExistsDelete(t *testing.T) {
	contextBackground := context.Background()

	testCases := []struct {
		name    string
		path    string
		key     string
		content string
	}{
		{
			name:    "simple key",
			path:    "taint",
			key:     "target",
			content: "content-1",
		},
		{
			name:    "label key with path separators",
			path:    "taint",
			key:     "//dbt/container:clickhouse-dbt-arm64",
			content: "content-2",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fileSystemCache := &FileSystemCache{
				workspaceCacheDir: t.TempDir(),
			}

			err := fileSystemCache.Set(
				contextBackground,
				testCase.path,
				testCase.key,
				strings.NewReader(testCase.content),
			)
			if err != nil {
				t.Fatalf("Set returned error: %v", err)
			}

			exists, err := fileSystemCache.Exists(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Exists returned error: %v", err)
			}
			if !exists {
				t.Fatalf("Exists returned false for key %q", testCase.key)
			}

			reader, err := fileSystemCache.Get(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Get returned error: %v", err)
			}
			defer reader.Close()

			contentBytes, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("failed reading cached content: %v", err)
			}
			if string(contentBytes) != testCase.content {
				t.Fatalf("expected %q, got %q", testCase.content, string(contentBytes))
			}

			err = fileSystemCache.Delete(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Delete returned error: %v", err)
			}

			exists, err = fileSystemCache.Exists(contextBackground, testCase.path, testCase.key)
			if err != nil {
				t.Fatalf("Exists after Delete returned error: %v", err)
			}
			if exists {
				t.Fatalf("expected key %q to be deleted", testCase.key)
			}
		})
	}
}
