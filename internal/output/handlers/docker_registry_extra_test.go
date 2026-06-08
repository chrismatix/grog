package handlers

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMakeRegistryAuth_NoConfig(t *testing.T) {
	dir := t.TempDir()
	prev := os.Getenv("DOCKER_CONFIG")
	t.Setenv("DOCKER_CONFIG", dir)
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONFIG", prev) })

	auth, err := makeRegistryAuth("gcr.io/x/y")
	if err != nil {
		t.Fatalf("makeRegistryAuth: %v", err)
	}
	if auth == "" {
		t.Fatal("empty auth")
	}

	raw, err := base64.URLEncoding.DecodeString(auth)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestMakeRegistryAuth_WithConfig(t *testing.T) {
	dir := t.TempDir()
	prev := os.Getenv("DOCKER_CONFIG")
	t.Setenv("DOCKER_CONFIG", dir)
	t.Cleanup(func() { _ = os.Setenv("DOCKER_CONFIG", prev) })

	cfg := `{
		"auths": {
			"gcr.io": {
				"auth": "dXNlcjpwYXNz"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	auth, err := makeRegistryAuth("gcr.io/repo/image")
	if err != nil {
		t.Fatalf("makeRegistryAuth: %v", err)
	}
	if auth == "" {
		t.Fatal("empty")
	}
}
