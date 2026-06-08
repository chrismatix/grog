package output

import (
	"testing"

	"grog/internal/proto/gen"
)

func TestGetOutputHash_Empty(t *testing.T) {
	h, err := getOutputHash(nil)
	if err != nil || h != "" {
		t.Fatalf("got %q %v", h, err)
	}
}

func TestGetOutputHash_OrderIndependent(t *testing.T) {
	a := &gen.Output{Kind: &gen.Output_File{File: &gen.FileOutput{Path: "a"}}}
	b := &gen.Output{Kind: &gen.Output_File{File: &gen.FileOutput{Path: "b"}}}
	h1, err := getOutputHash([]*gen.Output{a, b})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := getOutputHash([]*gen.Output{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("expected order independent: %q %q", h1, h2)
	}
}
