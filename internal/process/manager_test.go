package process

import (
	"strings"
	"testing"
	"time"
)

func TestFailureCapturesStderr(t *testing.T) {
	done := make(chan ExitEvent, 1)
	m := New(func(ev ExitEvent) {
		done <- ev
	})
	m.Ensure("boom", "", "echo 'missing env SECRET' >&2; exit 42")

	if err := m.Start("boom"); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-done:
		if ev.ExitCode != 42 {
			t.Fatalf("exit code = %d, want 42", ev.ExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for exit")
	}

	st, code, msg := m.Status("boom")
	if st != StatusFailed {
		t.Fatalf("status = %v, want failed", st)
	}
	if code != 42 {
		t.Fatalf("code = %d, want 42", code)
	}
	if !strings.Contains(msg, "missing env SECRET") {
		t.Fatalf("errMsg = %q, want stderr contents", msg)
	}
}

func TestLastNonEmptyLines(t *testing.T) {
	got := lastNonEmptyLines("a\n\nb\nc\n\n", 2)
	if got != "b\nc" {
		t.Fatalf("got %q", got)
	}
}
