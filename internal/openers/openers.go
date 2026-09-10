package openers

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Editor is a known folder opener.
type Editor struct {
	Name string // display name
	Bin  string // binary on PATH
}

var candidates = []Editor{
	{Name: "VS Code", Bin: "code"},
	{Name: "Cursor", Bin: "cursor"},
	{Name: "PHPStorm", Bin: "phpstorm"},
	{Name: "PHPStorm", Bin: "phpstorm.sh"},
	{Name: "WebStorm", Bin: "webstorm"},
	{Name: "IntelliJ IDEA", Bin: "idea"},
	{Name: "Sublime Text", Bin: "subl"},
	{Name: "Zed", Bin: "zed"},
	{Name: "Neovim", Bin: "nvim"},
	{Name: "Vim", Bin: "vim"},
	{Name: "Nano", Bin: "nano"},
	{Name: "Emacs", Bin: "emacs"},
}

var (
	availOnce sync.Once
	available []Editor
)

// Available returns editors found on PATH (cached for the process).
func Available() []Editor {
	availOnce.Do(func() {
		seen := make(map[string]bool)
		for _, c := range candidates {
			if seen[c.Bin] {
				continue
			}
			if _, err := exec.LookPath(c.Bin); err != nil {
				continue
			}
			seen[c.Bin] = true
			available = append(available, c)
		}
	})
	return available
}

// OpenFolder launches editorBin with folder as argument (non-blocking).
func OpenFolder(editorBin, folder string) error {
	if folder == "" {
		return fmt.Errorf("no folder configured")
	}
	if _, err := os.Stat(folder); err != nil {
		return fmt.Errorf("folder: %w", err)
	}
	cmd := exec.Command(editorBin, folder)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
