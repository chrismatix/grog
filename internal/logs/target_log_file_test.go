package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/label"
	"grog/internal/model"
)

func setupConfig(t *testing.T) {
	t.Helper()
	prev := config.Global
	tmp := t.TempDir()
	config.Global = config.WorkspaceConfig{Root: tmp, WorkspaceRoot: tmp}
	t.Cleanup(func() { config.Global = prev })
}

func makeTarget(pkg, name string) model.Target {
	return model.Target{Label: label.TL(pkg, name)}
}

func TestTargetLogFile_PathAndOpen(t *testing.T) {
	setupConfig(t)
	tf := NewTargetLogFile(makeTarget("pkg/sub", "t"))
	p := tf.Path()
	if !strings.HasSuffix(p, filepath.Join("logs", "pkg/sub", "t.txt")) {
		t.Fatalf("got %q", p)
	}

	if tf.Exists() {
		t.Fatal("expected not exists")
	}

	f, err := tf.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if !tf.Exists() {
		t.Fatal("expected exists")
	}
}

func TestTargetLogFile_Print(t *testing.T) {
	setupConfig(t)
	tf := NewTargetLogFile(makeTarget("pkg", "p"))
	f, err := tf.Open()
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("content")
	f.Close()

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })

	err = tf.Print()
	w.Close()
	if err != nil {
		t.Fatalf("Print: %v", err)
	}
	buf := make([]byte, 32)
	n, _ := r.Read(buf)
	if string(buf[:n]) != "content" {
		t.Fatalf("got %q", string(buf[:n]))
	}
}

func TestTargetLogFile_PrintMissing(t *testing.T) {
	setupConfig(t)
	tf := NewTargetLogFile(makeTarget("pkg", "missing"))
	if err := tf.Print(); err == nil {
		t.Fatal("expected err")
	}
}
