package hashing

import (
	"testing"

	"grog/internal/config"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"
)

func TestGetHasher_SelectsAlgorithm(t *testing.T) {
	prev := config.Global.HashAlgorithm
	t.Cleanup(func() { config.Global.HashAlgorithm = prev })

	cases := []struct {
		algo string
		typ  string
	}{
		{"", "*hashing.xxh3Hasher"},
		{"xxh3", "*hashing.xxh3Hasher"},
		{"sha256", "*hashing.sha256Hasher"},
		{"unknown", "*hashing.xxh3Hasher"},
	}
	for _, c := range cases {
		config.Global.HashAlgorithm = c.algo
		h := GetHasher()
		if h == nil {
			t.Fatalf("nil hasher for algo %q", c.algo)
		}
		if _, err := h.Write([]byte("a")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if _, err := h.WriteString("b"); err != nil {
			t.Fatalf("WriteString: %v", err)
		}
		if h.SumString() == "" {
			t.Fatalf("empty sum for %q", c.algo)
		}
	}
}

func TestNewSHA256HasherUsage(t *testing.T) {
	h := newSHA256Hasher()
	if _, err := h.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WriteString(" world"); err != nil {
		t.Fatal(err)
	}
	if h.SumString() == "" {
		t.Fatal("empty sum")
	}
}

func TestTargetHasher_SetExtraArgs(t *testing.T) {
	g := dag.NewDirectedGraph()
	th := NewTargetHasher(g)
	th.SetExtraArgs([]string{"--foo"})

	target := &model.Target{
		Label:      label.TL("pkg", "t"),
		Command:    "echo",
		OutputHash: "abc",
	}
	if err := th.SetTargetChangeHash(target); err != nil {
		t.Fatalf("SetTargetChangeHash: %v", err)
	}
	if target.ChangeHash == "" {
		t.Fatal("ChangeHash not set")
	}

	prevHash := target.ChangeHash
	if err := th.SetTargetChangeHash(target); err != nil {
		t.Fatalf("repeat call: %v", err)
	}
	if target.ChangeHash != prevHash {
		t.Fatal("expected idempotent")
	}
}
