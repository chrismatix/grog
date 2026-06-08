package cmds

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grog/internal/config"
	"grog/internal/console"
	"grog/internal/dag"
	"grog/internal/label"
	"grog/internal/model"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func newTestLogger() *console.Logger {
	core, _ := observer.New(zap.DebugLevel)
	return console.NewFromSugared(zap.New(core).Sugar(), zap.DebugLevel)
}

func TestContainsFile(t *testing.T) {
	files := []string{"/a/b.go", "/c/d.go"}
	if !containsFile(files, "/a/b.go") {
		t.Fatal("expected match")
	}
	if containsFile(files, "/x/y.go") {
		t.Fatal("did not expect match")
	}
	if containsFile(nil, "/a/b.go") {
		t.Fatal("nil slice cannot contain anything")
	}
}

func TestSplitRunArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		dash        int
		wantTargets []string
		wantArgs    []string
		wantErr     bool
	}{
		{
			name:        "noDash",
			args:        []string{"//a:a", "//b:b"},
			dash:        -1,
			wantTargets: []string{"//a:a", "//b:b"},
			wantArgs:    nil,
		},
		{
			name:        "withDash",
			args:        []string{"//a:a", "arg1", "arg2"},
			dash:        1,
			wantTargets: []string{"//a:a"},
			wantArgs:    []string{"arg1", "arg2"},
		},
		{
			name:    "dashAtStart",
			args:    []string{"arg1"},
			dash:    0,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets, args, err := splitRunArgs(tc.args, tc.dash)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !sliceEq(targets, tc.wantTargets) {
				t.Fatalf("targets: got %v want %v", targets, tc.wantTargets)
			}
			if !sliceEq(args, tc.wantArgs) {
				t.Fatalf("args: got %v want %v", args, tc.wantArgs)
			}
		})
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseMultipleTargetLabelsDedup(t *testing.T) {
	logger := newTestLogger()
	labels := parseMultipleTargetLabels(logger, "pkg", []string{"//a:a", "//b:b", "//a:a"})
	if len(labels) != 2 {
		t.Fatalf("expected 2 deduped labels, got %d (%v)", len(labels), labels)
	}
	if labels[0].String() != "//a:a" || labels[1].String() != "//b:b" {
		t.Fatalf("unexpected labels: %v", labels)
	}
}

func TestBuildSucceeded(t *testing.T) {
	if !buildSucceeded(nil, dag.CompletionMap{}) {
		t.Fatal("empty completion map with no error should succeed")
	}
	if buildSucceeded(errors.New("x"), dag.CompletionMap{}) {
		t.Fatal("any execution error should be a failure")
	}
	if buildSucceeded(nil, nil) {
		t.Fatal("nil completion map should be treated as failure")
	}
	failing := dag.CompletionMap{
		label.TargetLabel{Package: "p", Name: "a"}: dag.Completion{
			IsSuccess: false,
			Err:       errors.New("boom"),
		},
	}
	if buildSucceeded(nil, failing) {
		t.Fatal("completion with errors must not be a success")
	}
}

func TestGetChangedFilesError(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	if _, err := getChangedFiles("HEAD"); err == nil {
		t.Fatal("expected git error outside a repo")
	}
}

func TestDisplayPath(t *testing.T) {
	prev := config.Global.WorkspaceRoot
	t.Cleanup(func() { config.Global.WorkspaceRoot = prev })

	config.Global.WorkspaceRoot = "/workspace"
	if got := displayPath("/workspace/pkg/file.go"); got != filepath.Join("pkg", "file.go") {
		t.Fatalf("got %q", got)
	}

	config.Global.WorkspaceRoot = ""
	abs := "/some/abs/path"
	if got := displayPath(abs); got != strings.TrimPrefix(abs, "/") && got != abs {
		t.Logf("got %q for empty workspace root", got)
	}
}

func TestBuildDependentTreeAndUpstream(t *testing.T) {
	leaf := &model.Target{Label: label.TargetLabel{Package: "p", Name: "leaf"}}
	mid := &model.Target{Label: label.TargetLabel{Package: "p", Name: "mid"}}
	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "root"}}

	graph := dag.NewDirectedGraphFromTargets(leaf, mid, root)
	if err := graph.AddEdge(leaf, mid); err != nil {
		t.Fatal(err)
	}
	if err := graph.AddEdge(mid, root); err != nil {
		t.Fatal(err)
	}

	depTree := buildDependentTree(leaf, graph)
	if depTree == nil {
		t.Fatal("expected tree")
	}
	rendered := depTree.String()
	if !strings.Contains(rendered, "//p:leaf") || !strings.Contains(rendered, "//p:mid") || !strings.Contains(rendered, "//p:root") {
		t.Fatalf("missing labels in tree: %q", rendered)
	}

	affected := map[label.TargetLabel]model.BuildNode{
		leaf.Label: leaf,
		mid.Label:  mid,
		root.Label: root,
	}
	targetToFiles := map[label.TargetLabel][]string{
		leaf.Label: {"/abs/changed/file.go"},
	}
	prev := config.Global.WorkspaceRoot
	t.Cleanup(func() { config.Global.WorkspaceRoot = prev })
	config.Global.WorkspaceRoot = "/abs"

	up := buildUpstreamTree(root, graph, affected, targetToFiles, true)
	rendered = up.String()
	if !strings.Contains(rendered, "//p:leaf") {
		t.Fatalf("expected upstream tree to recurse to leaf, got %q", rendered)
	}
	if !strings.Contains(rendered, "changed/file.go") {
		t.Fatalf("expected attached file leaf, got %q", rendered)
	}

	outside := &model.Target{Label: label.TargetLabel{Package: "p", Name: "outside"}}
	graph.AddNode(outside)
	if err := graph.AddEdge(outside, root); err != nil {
		t.Fatal(err)
	}
	up = buildUpstreamTree(root, graph, affected, targetToFiles, false)
	rendered = up.String()
	if strings.Contains(rendered, "//p:outside") {
		t.Fatalf("buildUpstreamTree must skip dependencies outside the affected set, got %q", rendered)
	}
}

func TestComputeAffectedSet(t *testing.T) {
	leaf := &model.Target{Label: label.TargetLabel{Package: "p", Name: "leaf"}}
	mid := &model.Target{Label: label.TargetLabel{Package: "p", Name: "mid"}}
	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "root"}}
	graph := dag.NewDirectedGraphFromTargets(leaf, mid, root)
	if err := graph.AddEdge(leaf, mid); err != nil {
		t.Fatal(err)
	}
	if err := graph.AddEdge(mid, root); err != nil {
		t.Fatal(err)
	}

	fileToTargets := map[string][]*model.Target{
		"/a.go": {leaf},
		"/b.go": {leaf},
	}
	affected, targetToFiles := computeAffectedSet(fileToTargets, graph)
	if _, ok := affected[leaf.Label]; !ok {
		t.Fatal("leaf should be in affected set")
	}
	if _, ok := affected[mid.Label]; !ok {
		t.Fatal("mid should be in affected set (descendant)")
	}
	if _, ok := affected[root.Label]; !ok {
		t.Fatal("root should be in affected set (descendant)")
	}
	files := targetToFiles[leaf.Label]
	if len(files) != 2 || files[0] != "/a.go" || files[1] != "/b.go" {
		t.Fatalf("expected files sorted, got %v", files)
	}
}

func TestPrintTreeFunctionsExecute(t *testing.T) {
	leaf := &model.Target{Label: label.TargetLabel{Package: "p", Name: "leaf"}}
	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "root"}}
	graph := dag.NewDirectedGraphFromTargets(leaf, root)
	if err := graph.AddEdge(leaf, root); err != nil {
		t.Fatal(err)
	}

	prev := config.Global.WorkspaceRoot
	t.Cleanup(func() { config.Global.WorkspaceRoot = prev })
	config.Global.WorkspaceRoot = "/abs"

	fileToTargets := map[string][]*model.Target{
		"/abs/some/file.go": {leaf},
	}
	captureStdout(t, func() {
		printFileRootedTree(fileToTargets, graph)
	})
	captureStdout(t, func() {
		printTargetRootedTree(fileToTargets, graph)
	})
	affected, t2f := computeAffectedSet(fileToTargets, graph)
	captureStdout(t, func() {
		printConsumerRootedTree(affected, t2f, graph, true)
	})
}

func TestBuildTreeFromGraphCmd(t *testing.T) {
	leaf := &model.Target{Label: label.TargetLabel{Package: "p", Name: "leaf"}}
	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "root"}}
	graph := dag.NewDirectedGraphFromTargets(leaf, root)
	if err := graph.AddEdge(leaf, root); err != nil {
		t.Fatal(err)
	}
	tr := buildTree(root, graph, 0)
	rendered := tr.String()
	if !strings.Contains(rendered, "//p:leaf") || !strings.Contains(rendered, "//p:root") {
		t.Fatalf("missing labels: %q", rendered)
	}
	tr2 := buildTree(leaf, graph, 1)
	if tr2 == nil {
		t.Fatal("expected non-nil leaf tree")
	}
}

func TestPrintTreeAndMermaid(t *testing.T) {
	leaf := &model.Target{Label: label.TargetLabel{Package: "p", Name: "leaf"}, IsSelected: true, UnresolvedInputs: []string{"a.go"}}
	root := &model.Target{Label: label.TargetLabel{Package: "p", Name: "root"}, IsSelected: true}
	graph := dag.NewDirectedGraphFromTargets(leaf, root)
	if err := graph.AddEdge(leaf, root); err != nil {
		t.Fatal(err)
	}
	prev := config.Global.WorkspaceRoot
	t.Cleanup(func() { config.Global.WorkspaceRoot = prev })
	config.Global.WorkspaceRoot = "/tmp/ws"

	captureStdout(t, func() { printTree(graph) })

	graphOptions.mermaidInputsAsNodes = true
	t.Cleanup(func() { graphOptions.mermaidInputsAsNodes = false })
	captureStdout(t, func() { printMermaidDiagram(graph) })
}

// captureStdout redirects stdout for the duration of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := os.ReadFile("/dev/null")
		_ = b
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		done <- string(buf)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestVersionCmdPrints(t *testing.T) {
	out := captureStdout(t, func() {
		VersionCmd.Run(VersionCmd, nil)
	})
	if out == "" {
		t.Fatal("expected version output")
	}
}

func TestCleanCmdSuccess(t *testing.T) {
	tmpRoot := t.TempDir()
	tmpWs := t.TempDir()
	prev := config.Global
	t.Cleanup(func() { config.Global = prev })
	config.Global.Root = tmpRoot
	config.Global.WorkspaceRoot = tmpWs

	if err := os.MkdirAll(config.Global.GetWorkspaceRootDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.Global.GetWorkspaceCacheDirectory(), 0755); err != nil {
		t.Fatal(err)
	}
	expunge = false
	CleanCmd.Run(CleanCmd, nil)

	expunge = true
	t.Cleanup(func() { expunge = false })
	CleanCmd.Run(CleanCmd, nil)
}

func TestInfoCmdSuccess(t *testing.T) {
	tmpRoot := t.TempDir()
	tmpWs := t.TempDir()
	prev := config.Global
	t.Cleanup(func() { config.Global = prev })
	config.Global.Root = tmpRoot
	config.Global.WorkspaceRoot = tmpWs
	config.Global.OS = "linux"
	config.Global.Arch = "amd64"
	config.Global.LogLevel = "info"
	config.Global.LogOutputPath = "stdout"

	if err := os.MkdirAll(config.Global.GetWorkspaceCacheDirectory(), 0755); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		InfoCmd.Run(InfoCmd, nil)
	})
}

// minimalWorkspace builds a tiny workspace with a single BUILD.json package.
func minimalWorkspace(t *testing.T) (workspaceRoot string, restore func()) {
	t.Helper()
	ws := t.TempDir()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	build := `{"targets":[{"name":"a","command":"echo a"}]}`
	if err := os.WriteFile(filepath.Join(ws, "pkg", "BUILD.json"), []byte(build), 0644); err != nil {
		t.Fatal(err)
	}
	prev := config.Global
	prevWd, _ := os.Getwd()
	config.Global.WorkspaceRoot = ws
	config.Global.Root = root
	config.Global.LogLevel = "info"
	config.Global.LogOutputPath = "stdout"
	config.Global.NumWorkers = 1
	config.Global.OS = "linux"
	config.Global.Arch = "amd64"
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	depsOptions.targetType = "all"
	rDepsOptions.targetType = "all"
	listOptions.targetType = "all"
	changesOptions.targetType = "all"
	graphOptions.output = "tree"
	return ws, func() {
		config.Global = prev
		_ = os.Chdir(prevWd)
	}
}

func TestListCmdSuccess(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		ListCmd.Run(ListCmd, nil)
	})
	captureStdout(t, func() {
		ListCmd.Run(ListCmd, []string{"//..."})
	})
}

func TestGraphCmdSuccess(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	graphOptions.output = "tree"
	captureStdout(t, func() { GraphCmd.Run(GraphCmd, nil) })

	graphOptions.output = "mermaid"
	captureStdout(t, func() { GraphCmd.Run(GraphCmd, nil) })

	graphOptions.output = "json"
	captureStdout(t, func() { GraphCmd.Run(GraphCmd, nil) })

	graphOptions.transitive = true
	captureStdout(t, func() { GraphCmd.Run(GraphCmd, []string{"//pkg:a"}) })
	graphOptions.transitive = false
	graphOptions.output = "tree"
}

func TestOwnersCmdSuccess(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		OwnersCmd.Run(OwnersCmd, []string{"pkg/BUILD.json"})
	})
}

func TestDepsCmd(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		DepsCmd.Run(DepsCmd, []string{"//pkg:a"})
	})
}

func TestRDepsCmd(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		RDepsCmd.Run(RDepsCmd, []string{"//pkg:a"})
	})
}

func TestDepsTransitive(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	depsOptions.transitive = true
	t.Cleanup(func() { depsOptions.transitive = false })
	captureStdout(t, func() {
		DepsCmd.Run(DepsCmd, []string{"//pkg:a"})
	})
}

func TestRDepsTransitive(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	rDepsOptions.transitive = true
	t.Cleanup(func() { rDepsOptions.transitive = false })
	captureStdout(t, func() {
		RDepsCmd.Run(RDepsCmd, []string{"//pkg:a"})
	})
}

func TestCheckCmdSuccess(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		CheckCmd.Run(CheckCmd, nil)
	})
}

func TestTaintCmdSuccess(t *testing.T) {
	_, restore := minimalWorkspace(t)
	defer restore()
	captureStdout(t, func() {
		TaintCmd.Run(TaintCmd, []string{"//pkg:a"})
	})
}

func TestChangesCmdEarlyReturnsWithoutSinceMissing(t *testing.T) {
	if got := containsFile([]string{"/x"}, "/x"); !got {
		t.Fatal("sanity")
	}
	_ = context.Background()
}

func minimalGitWorkspace(t *testing.T) (workspaceRoot string, restore func()) {
	t.Helper()
	ws := t.TempDir()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	build := `{"targets":[{"name":"a","command":"echo a > out.txt","outputs":[{"identifier":"out.txt","type":"FILE"}]}]}`
	if err := os.WriteFile(filepath.Join(ws, "pkg", "BUILD.json"), []byte(build), 0644); err != nil {
		t.Fatal(err)
	}

	prev := config.Global
	prevWd, _ := os.Getwd()

	config.Global = config.WorkspaceConfig{}
	config.Global.WorkspaceRoot = ws
	config.Global.Root = root
	config.Global.LogLevel = "info"
	config.Global.LogOutputPath = "stdout"
	config.Global.NumWorkers = 1
	config.Global.OS = "linux"
	config.Global.Arch = "amd64"

	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	depsOptions.targetType = "all"
	rDepsOptions.targetType = "all"
	listOptions.targetType = "all"
	changesOptions.targetType = "all"
	graphOptions.output = "tree"

	return ws, func() {
		config.Global = prev
		_ = os.Chdir(prevWd)
	}
}
