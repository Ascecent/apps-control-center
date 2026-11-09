package gitinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectNotGit(t *testing.T) {
	dir := t.TempDir()
	info := Detect(dir)
	if info.OK {
		t.Fatalf("expected not OK, got %+v", info)
	}
}

func TestDetectGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "init")
	run("branch", "-M", "main")

	info := Detect(dir)
	if !info.OK {
		t.Fatalf("expected OK, got %+v", info)
	}
	if info.Name != filepath.Base(dir) {
		t.Fatalf("name = %q", info.Name)
	}
	if info.Branch != "main" {
		t.Fatalf("branch = %q", info.Branch)
	}
	if info.String() != info.Name+" @ main" {
		t.Fatalf("String = %q", info.String())
	}
}
