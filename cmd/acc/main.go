package main

import (
	"fmt"
	"os"

	"github.com/ascecent/apps-control-center/internal/config"
	"github.com/ascecent/apps-control-center/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	path := config.DefaultPath
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "create %s with lines like:\n  [profile]\n  name: folder | command\n", path)
		os.Exit(1)
	}

	// Start on the first selectable profile (skip utilities).
	activeProfile := config.DefaultProfile
	if names := cfg.ProfileNames(); len(names) > 0 {
		activeProfile = names[0]
	}

	var p *tea.Program
	model := ui.New(path, cfg, activeProfile, func(msg tea.Msg) {
		if p != nil {
			p.Send(msg)
		}
	})
	p = tea.NewProgram(model, tea.WithAltScreen())

	if err := model.StartWatcher(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config watch disabled: %v\n", err)
	}
	defer model.Close()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
