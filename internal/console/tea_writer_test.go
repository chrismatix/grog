package console

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCollapseCarriageReturns(t *testing.T) {
	cases := map[string]string{
		"plain":             "plain",
		"10%\r20%\r100%":    "100%",
		"10%\r20%\r100%\r":  "100%",
		"\rfoo":             "foo",
		"prefix\roverwrite": "overwrite",
		"":                  "",
	}
	for in, want := range cases {
		if got := collapseCarriageReturns(in); got != want {
			t.Errorf("collapseCarriageReturns(%q) = %q, want %q", in, got, want)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what was written.
// UseTea() is false under test (stdout is a pipe), so TeaWriter.emit uses
// fmt.Println and never touches its (nil) tea.Program.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(bufio.NewReader(r))
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

func TestTeaWriterReassemblesFragmentedLines(t *testing.T) {
	out := captureStdout(t, func() {
		w := &TeaWriter{}
		// A single logical line split across writes must emit as one line.
		_, _ = w.Write([]byte("hello "))
		_, _ = w.Write([]byte("world\n"))
		// Multiple lines in one write split correctly.
		_, _ = w.Write([]byte("a\nb\n"))
		// Carriage-return progress collapses to one line, not many.
		_, _ = w.Write([]byte("10%\r20%\r100%\n"))
		// Trailing partial line stays buffered until Flush.
		_, _ = w.Write([]byte("no newline yet"))
		w.Flush()
	})

	want := []string{"hello world", "a", "b", "100%", "no newline yet"}
	got := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTeaWriterFlushNoopWhenEmpty(t *testing.T) {
	out := captureStdout(t, func() {
		w := &TeaWriter{}
		_, _ = w.Write([]byte("done\n"))
		w.Flush() // nothing buffered; must not emit a blank line
	})
	if out != "done\n" {
		t.Errorf("got %q, want %q", out, "done\n")
	}
}
