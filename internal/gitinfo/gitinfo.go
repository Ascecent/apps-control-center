package gitinfo

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Info describes a git work tree rooted at or above Folder.
type Info struct {
	Root   string // absolute repo root
	Name   string // basename of root
	Branch string
	OK     bool
}

// Detect returns git info for folder, or OK=false if not a work tree / git missing.
func Detect(folder string) Info {
	if folder == "" {
		return Info{}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return Info{}
	}

	root, err := gitOutput(folder, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return Info{}
	}

	branch, _ := gitOutput(folder, "branch", "--show-current")
	if branch == "" {
		// Detached HEAD — show short SHA.
		if sha, err := gitOutput(folder, "rev-parse", "--short", "HEAD"); err == nil {
			branch = "detached@" + sha
		}
	}

	return Info{
		Root:   root,
		Name:   filepath.Base(root),
		Branch: branch,
		OK:     true,
	}
}

// String formats a short label for the UI, e.g. "api @ main".
func (i Info) String() string {
	if !i.OK {
		return ""
	}
	if i.Branch == "" {
		return i.Name
	}
	return i.Name + " @ " + i.Branch
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
