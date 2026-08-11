package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeDockerConfig(t *testing.T, auths string) {
	t.Helper()
	configDir := t.TempDir()
	config := fmt.Sprintf(`{"auths":{%s}}`, auths)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0600); err != nil {
		t.Fatalf("write docker config: %v", err)
	}
	t.Setenv("DOCKER_CONFIG", configDir)
}

func decodeRegistryAuth(t *testing.T, header string) (username, password string) {
	t.Helper()
	raw, err := base64.URLEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode auth header: %v", err)
	}
	var authConfig struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &authConfig); err != nil {
		t.Fatalf("unmarshal auth header %q: %v", raw, err)
	}
	return authConfig.Username, authConfig.Password
}

func encodeAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

// A shorthand destination names a Docker Hub repository, not a registry host,
// so splitting it on "/" would look up credentials for "alice".
func TestMakeRegistryAuthResolvesShorthandToDockerHub(t *testing.T) {
	writeDockerConfig(t, fmt.Sprintf(`"https://index.docker.io/v1/":{"auth":%q}`, encodeAuth("alice", "hunter2")))

	header, err := makeRegistryAuth("alice/app:latest")
	if err != nil {
		t.Fatalf("makeRegistryAuth: %v", err)
	}

	username, password := decodeRegistryAuth(t, header)
	if username != "alice" || password != "hunter2" {
		t.Errorf("resolved %q/%q, want the Docker Hub credentials alice/hunter2", username, password)
	}
}

func TestMakeRegistryAuthResolvesExplicitHost(t *testing.T) {
	writeDockerConfig(t, fmt.Sprintf(
		`"myregistry.example.com":{"auth":%q},"https://index.docker.io/v1/":{"auth":%q}`,
		encodeAuth("robot", "s3cret"), encodeAuth("alice", "hunter2"),
	))

	header, err := makeRegistryAuth("myregistry.example.com/team/app:1")
	if err != nil {
		t.Fatalf("makeRegistryAuth: %v", err)
	}

	username, password := decodeRegistryAuth(t, header)
	if username != "robot" || password != "s3cret" {
		t.Errorf("resolved %q/%q, want the host credentials robot/s3cret", username, password)
	}
}

func TestMakeRegistryAuthWithoutCredentials(t *testing.T) {
	writeDockerConfig(t, "")

	header, err := makeRegistryAuth("myregistry.example.com/team/app:1")
	if err != nil {
		t.Fatalf("makeRegistryAuth: %v", err)
	}

	username, password := decodeRegistryAuth(t, header)
	if username != "" || password != "" {
		t.Errorf("resolved %q/%q for an unauthenticated registry, want empty", username, password)
	}
}
