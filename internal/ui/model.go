package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ascecent/apps-control-center/internal/config"
	"github.com/ascecent/apps-control-center/internal/openers"
	"github.com/ascecent/apps-control-center/internal/process"
	"github.com/ascecent/apps-control-center/internal/watcher"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type exitMsg process.ExitEvent
type configReloadMsg struct{}
type statusTickMsg time.Time

// listEntry is one row in the combined list (profile app or utility).
type listEntry struct {
	App     config.App
	Utility bool
}

type Model struct {
	path          string
	cfg           *config.Config
	activeProfile string
	profileApps   []config.App
	utilityApps   []config.App
	entries       []listEntry // combined view order: profile then utilities
	cursor        int
	mgr           *process.Manager
	watch         *watcher.Watch
	status        string
	errFlash      string
	width         int
	height        int
	quitting      bool
	send          func(tea.Msg)
	selfUse       process.Usage

	editing bool
	editor  textarea.Model

	picking     bool
	pickEditors []openers.Editor
	pickCursor  int
	pickFolder  string
	pickApp     string
	lastEditor  string

	pickingProfile bool
	profileCursor  int
}

func New(path string, cfg *config.Config, activeProfile string, send func(tea.Msg)) *Model {
	m := &Model{
		path:          path,
		cfg:           cfg,
		activeProfile: activeProfile,
		send:          send,
	}
	m.mgr = process.New(func(ev process.ExitEvent) {
		if send != nil {
			send(exitMsg(ev))
		}
	})
	m.profileApps = cfg.AppsFor(activeProfile)
	m.utilityApps = append([]config.App(nil), cfg.Utilities...)
	m.rebuildEntries()
	for _, a := range m.profileApps {
		m.mgr.Ensure(a.Name, a.Folder, a.Command)
	}
	for _, a := range m.utilityApps {
		m.mgr.Ensure(a.Name, a.Folder, a.Command)
	}
	return m
}

func (m *Model) rebuildEntries() {
	m.entries = m.entries[:0]
	for _, a := range m.profileApps {
		m.entries = append(m.entries, listEntry{App: a, Utility: false})
	}
	for _, a := range m.utilityApps {
		m.entries = append(m.entries, listEntry{App: a, Utility: true})
	}
	if m.cursor >= len(m.entries) {
		m.cursor = max(0, len(m.entries)-1)
	}
}

func (m *Model) StartWatcher() error {
	w, err := watcher.Start(m.path, 400*time.Millisecond, func() {
		if m.send != nil {
			m.send(configReloadMsg{})
		}
	})
	if err != nil {
		return err
	}
	m.watch = w
	return nil
}

func (m *Model) Close() {
	if m == nil {
		return
	}
	if m.watch != nil {
		m.watch.Close()
		m.watch = nil
	}
	if m.mgr != nil {
		m.mgr.StopAll()
	}
}

func (m Model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return statusTickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutEditor()
		return m, nil

	case statusTickMsg:
		m.selfUse = process.SelfUsage()
		return m, tick()

	case exitMsg:
		if msg.Err != nil {
			m.status = fmt.Sprintf("%s failed (%d)", msg.Name, msg.ExitCode)
		} else {
			m.status = fmt.Sprintf("%s exited (%d)", msg.Name, msg.ExitCode)
		}
		return m, nil

	case configReloadMsg:
		if m.editing {
			return m, nil
		}
		return m.reloadConfig()

	case tea.KeyMsg:
		if m.pickingProfile {
			return m.updateProfilePicker(msg)
		}
		if m.picking {
			return m.updateEditorPicker(msg)
		}
		if m.editing {
			return m.updateEditor(msg)
		}
		return m.updateList(msg)
	}

	if m.editing {
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		m.Close()
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case "s":
		if app, ok := m.selected(); ok {
			if err := m.mgr.Start(app.Name); err != nil {
				m.status = err.Error()
			} else {
				m.status = fmt.Sprintf("started %s", app.Name)
			}
		}
	case "d", "x":
		if app, ok := m.selected(); ok {
			if err := m.mgr.Stop(app.Name); err != nil {
				m.status = err.Error()
			} else {
				m.status = fmt.Sprintf("stopped %s", app.Name)
			}
		}
	case "r":
		if app, ok := m.selected(); ok {
			if err := m.mgr.Restart(app.Name); err != nil {
				m.status = err.Error()
			} else {
				m.status = fmt.Sprintf("restarted %s", app.Name)
			}
		}
	case "o":
		if app, ok := m.selected(); ok {
			u := m.mgr.URL(app.Name)
			if u == "" {
				m.status = "no URL detected yet"
				break
			}
			if err := openURL(u); err != nil {
				m.status = err.Error()
			} else {
				m.status = "opened " + u
			}
		}
	case "ctrl+e":
		return m.openEditorPicker()
	case "e":
		return m.openConfigEditor()
	case "p":
		return m.openProfilePicker()
	case "R":
		return m.reloadConfig()
	}
	return m, nil
}

func (m Model) selected() (config.App, bool) {
	if len(m.entries) == 0 || m.cursor < 0 || m.cursor >= len(m.entries) {
		return config.App{}, false
	}
	return m.entries[m.cursor].App, true
}

// ── profile picker ────────────────────────────────────────────────────────────

func (m Model) openProfilePicker() (tea.Model, tea.Cmd) {
	names := m.cfg.ProfileNames()
	if len(names) == 0 {
		m.status = "no profiles configured"
		return m, nil
	}
	m.pickingProfile = true
	m.profileCursor = 0
	for i, name := range names {
		if name == m.activeProfile {
			m.profileCursor = i
			break
		}
	}
	m.errFlash = ""
	m.status = "choose profile"
	return m, nil
}

func (m Model) updateProfilePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	names := m.cfg.ProfileNames()
	switch msg.String() {
	case "esc", "q":
		m.pickingProfile = false
		m.status = ""
		return m, nil
	case "up", "k":
		if m.profileCursor > 0 {
			m.profileCursor--
		}
	case "down", "j":
		if m.profileCursor < len(names)-1 {
			m.profileCursor++
		}
	case "enter":
		if len(names) == 0 {
			m.pickingProfile = false
			return m, nil
		}
		chosen := names[m.profileCursor]
		if chosen == m.activeProfile {
			m.pickingProfile = false
			m.status = ""
			return m, nil
		}
		return m.switchProfile(chosen)
	case "ctrl+c":
		m.quitting = true
		m.Close()
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) switchProfile(name string) (tea.Model, tea.Cmd) {
	// Stop only profile apps — utilities keep running.
	for _, a := range m.profileApps {
		m.mgr.Remove(a.Name)
	}

	m.activeProfile = name
	m.profileApps = m.cfg.AppsFor(name)
	for _, a := range m.profileApps {
		m.mgr.Ensure(a.Name, a.Folder, a.Command)
	}
	m.rebuildEntries()
	m.cursor = 0
	m.pickingProfile = false
	m.errFlash = ""
	m.status = fmt.Sprintf("profile: %s", name)
	return m, nil
}

// ── editor (folder opener) picker ─────────────────────────────────────────────

func (m Model) openEditorPicker() (tea.Model, tea.Cmd) {
	app, ok := m.selected()
	if !ok {
		return m, nil
	}
	if app.Folder == "" {
		m.status = "no folder configured for " + app.Name
		return m, nil
	}
	eds := openers.Available()
	if len(eds) == 0 {
		m.status = "no editors found on PATH"
		return m, nil
	}
	m.pickEditors = eds
	m.pickFolder = app.Folder
	m.pickApp = app.Name
	m.pickCursor = 0
	if m.lastEditor != "" {
		for i, ed := range eds {
			if ed.Bin == m.lastEditor {
				m.pickCursor = i
				break
			}
		}
	}
	m.picking = true
	m.errFlash = ""
	m.status = "choose editor"
	return m, nil
}

func (m Model) updateEditorPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.picking = false
		m.status = "open cancelled"
		return m, nil
	case "up", "k":
		if m.pickCursor > 0 {
			m.pickCursor--
		}
	case "down", "j":
		if m.pickCursor < len(m.pickEditors)-1 {
			m.pickCursor++
		}
	case "enter":
		if len(m.pickEditors) == 0 {
			m.picking = false
			m.status = "no editors found"
			return m, nil
		}
		ed := m.pickEditors[m.pickCursor]
		if err := openers.OpenFolder(ed.Bin, m.pickFolder); err != nil {
			m.errFlash = err.Error()
			m.status = "open failed"
			return m, nil
		}
		m.lastEditor = ed.Bin
		m.picking = false
		m.errFlash = ""
		m.status = fmt.Sprintf("opened %s with %s", m.pickApp, ed.Name)
		return m, nil
	case "ctrl+c":
		m.quitting = true
		m.Close()
		return m, tea.Quit
	}
	return m, nil
}

// ── config editor ─────────────────────────────────────────────────────────────

func (m Model) openConfigEditor() (tea.Model, tea.Cmd) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		m.errFlash = err.Error()
		m.status = "could not open config"
		return m, nil
	}

	ta := textarea.New()
	ta.Placeholder = "# [utilities]\n# [profile]\n# name: folder | command"
	ta.CharLimit = 0
	ta.ShowLineNumbers = true
	ta.Prompt = ""
	ta.SetValue(string(data))
	ta.Focus()
	ta.CursorEnd()

	m.editor = ta
	m.editing = true
	m.errFlash = ""
	m.status = "editing config"
	m.layoutEditor()
	return m, textarea.Blink
}

func (m Model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editing = false
		m.status = "edit cancelled"
		m.errFlash = ""
		return m, nil
	case "ctrl+s":
		return m.saveEditor()
	case "ctrl+c":
		m.quitting = true
		m.Close()
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m Model) saveEditor() (tea.Model, tea.Cmd) {
	content := m.editor.Value()
	cfg, err := config.Save(m.path, content)
	if err != nil {
		m.errFlash = err.Error()
		m.status = "save failed — fix errors and Ctrl+S again"
		return m, nil
	}
	m.editing = false
	return m.applyConfig(cfg, "config saved & applied")
}

func (m *Model) layoutEditor() {
	if !m.editing {
		return
	}
	listW, editorW, editorH := m.paneSizes()
	_ = listW
	m.editor.SetWidth(max(20, editorW-2))
	m.editor.SetHeight(max(5, editorH))
}

func (m Model) paneSizes() (listW, editorW, editorH int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	listW = w * 2 / 5
	if listW < 28 {
		listW = min(28, w/2)
	}
	if listW > w-24 {
		listW = max(20, w-24)
	}
	editorW = w - listW - 1
	editorH = h - 6
	if editorH < 5 {
		editorH = 5
	}
	return listW, editorW, editorH
}

// ── config reload / apply ─────────────────────────────────────────────────────

func (m Model) reloadConfig() (tea.Model, tea.Cmd) {
	cfg, err := config.Load(m.path)
	if err != nil {
		m.errFlash = err.Error()
		m.status = "config reload failed"
		return m, nil
	}
	return m.applyConfig(cfg, "config reloaded")
}

func reconcileApps(mgr *process.Manager, prev, next []config.App) {
	prevMap := make(map[string]config.App, len(prev))
	for _, a := range prev {
		prevMap[a.Name] = a
	}
	nextMap := make(map[string]config.App, len(next))
	for _, a := range next {
		nextMap[a.Name] = a
	}
	for name, old := range prevMap {
		if _, ok := nextMap[name]; !ok {
			mgr.Remove(name)
			continue
		}
		n := nextMap[name]
		mgr.Ensure(name, n.Folder, n.Command)
		if n.Command != old.Command || n.Folder != old.Folder {
			st, _, _ := mgr.Status(name)
			if st == process.StatusRunning || st == process.StatusRestarting {
				_ = mgr.Restart(name)
			}
		}
	}
	for name, a := range nextMap {
		if _, ok := prevMap[name]; !ok {
			mgr.Ensure(name, a.Folder, a.Command)
		}
	}
}

func (m Model) applyConfig(cfg *config.Config, status string) (tea.Model, tea.Cmd) {
	m.cfg = cfg

	if !cfg.HasProfile(m.activeProfile) {
		names := cfg.ProfileNames()
		oldProfile := m.activeProfile
		if len(names) > 0 {
			m.activeProfile = names[0]
		} else {
			m.activeProfile = config.DefaultProfile
		}
		for _, a := range m.profileApps {
			m.mgr.Remove(a.Name)
		}
		m.profileApps = cfg.AppsFor(m.activeProfile)
		for _, a := range m.profileApps {
			m.mgr.Ensure(a.Name, a.Folder, a.Command)
		}
		reconcileApps(m.mgr, m.utilityApps, cfg.Utilities)
		m.utilityApps = append([]config.App(nil), cfg.Utilities...)
		m.rebuildEntries()
		m.cursor = 0
		m.errFlash = ""
		m.status = fmt.Sprintf("%s (profile %q gone → %q)", status, oldProfile, m.activeProfile)
		return m, nil
	}

	newProfile := cfg.AppsFor(m.activeProfile)
	reconcileApps(m.mgr, m.profileApps, newProfile)
	reconcileApps(m.mgr, m.utilityApps, cfg.Utilities)
	m.profileApps = newProfile
	m.utilityApps = append([]config.App(nil), cfg.Utilities...)
	m.rebuildEntries()
	m.errFlash = ""
	m.status = status
	return m, nil
}

func openURL(u string) error {
	cmd := exec.Command("xdg-open", u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && (p == home || strings.HasPrefix(p, home+string(os.PathSeparator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.pickingProfile {
		return m.viewProfilePicker()
	}
	if m.picking {
		return m.viewEditorPicker()
	}
	if m.editing {
		return m.viewEditing()
	}
	return m.viewList(false)
}

func (m Model) viewProfilePicker() string {
	names := m.cfg.ProfileNames()
	var b strings.Builder
	b.WriteString(titleStyle.Render("Switch Profile"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("utilities stay running"))
	b.WriteString("\n\n")
	for i, name := range names {
		cursor := "  "
		style := itemStyle
		if i == m.profileCursor {
			cursor = "> "
			style = selectedStyle
		}
		suffix := ""
		if name == m.activeProfile {
			suffix = profileActiveStyle.Render("  ← active")
		}
		b.WriteString(style.Render(cursor+name) + suffix)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ select  Enter switch  Esc cancel"))
	b.WriteString("\n")
	return b.String()
}

func (m Model) viewEditorPicker() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Open folder in editor"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%s  →  %s", m.pickApp, m.pickFolder)))
	b.WriteString("\n\n")

	if len(m.pickEditors) == 0 {
		b.WriteString(errorStyle.Render("No editors found on PATH"))
		b.WriteString("\n")
	} else {
		for i, ed := range m.pickEditors {
			cursor := "  "
			style := itemStyle
			if i == m.pickCursor {
				cursor = "> "
				style = selectedStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%s  (%s)", cursor, ed.Name, ed.Bin)))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ select  Enter open  Esc cancel"))
	b.WriteString("\n")
	if m.errFlash != "" {
		b.WriteString(errorStyle.Render(m.errFlash))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) viewEditing() string {
	listW, editorW, _ := m.paneSizes()
	listPane := lipgloss.NewStyle().Width(listW).MaxHeight(m.height).Render(m.viewList(true))
	editorTitle := editorTitleStyle.Render("edit " + m.path)
	help := helpStyle.Render("Ctrl+S save & apply  Esc cancel")
	body := m.editor.View()
	inner := editorTitle + "\n" + body + "\n" + help
	if m.errFlash != "" {
		inner += "\n" + errorStyle.Render(m.errFlash)
	} else if m.status != "" {
		inner += "\n" + mutedStyle.Render(m.status)
	}
	editorPane := editorBorder.Width(max(20, editorW)).Render(inner)
	return lipgloss.JoinHorizontal(lipgloss.Top, listPane, editorPane)
}

func (m Model) viewList(compact bool) string {
	title := titleStyle.Render("Apps Control Center")
	profileBadge := profileStyle.Render(" " + m.activeProfile + " ")
	selfLine := mutedStyle.Render("acc  " + process.FormatUsage(m.selfUse))
	if !compact {
		selfLine = mutedStyle.Render("acc  " + process.FormatUsage(m.selfUse) + "   config: " + m.path)
	}

	var b strings.Builder
	b.WriteString(title + "  " + profileBadge)
	b.WriteString("\n")
	b.WriteString(selfLine)
	b.WriteString("\n\n")

	cmdMax := 36
	if compact {
		cmdMax = 18
	}

	if len(m.entries) == 0 {
		b.WriteString(mutedStyle.Render("No apps in this profile."))
		b.WriteString("\n")
	} else {
		nameW := 12
		for _, e := range m.entries {
			if len(e.App.Name) > nameW {
				nameW = len(e.App.Name)
			}
		}
		if nameW > 24 {
			nameW = 24
		}
		if compact && nameW > 14 {
			nameW = 14
		}

		wroteProfileHeader := false
		wroteUtilHeader := false

		for i, e := range m.entries {
			if !e.Utility && !wroteProfileHeader {
				b.WriteString(sectionStyle.Render("── " + m.activeProfile + " "))
				b.WriteString("\n")
				if !compact {
					b.WriteString(helpStyle.Render(fmt.Sprintf("  %-*s  %-8s  %-12s  %s", nameW, "APP", "STATUS", "CPU  MEM", "COMMAND")))
					b.WriteString("\n")
				}
				wroteProfileHeader = true
			}
			if e.Utility && !wroteUtilHeader {
				if wroteProfileHeader {
					b.WriteString("\n")
				}
				b.WriteString(sectionStyle.Render("── utilities "))
				b.WriteString("\n")
				wroteUtilHeader = true
			}

			a := e.App
			st, code, errMsg := m.mgr.Status(a.Name)
			appURL := ""
			usage := process.Usage{}
			if st == process.StatusRunning {
				appURL = m.mgr.URL(a.Name)
				usage = m.mgr.Usage(a.Name)
			}
			cursor := "  "
			lineStyle := itemStyle
			if i == m.cursor {
				cursor = "> "
				lineStyle = selectedStyle
			}
			cmd := a.Command
			if len(cmd) > cmdMax {
				cmd = cmd[:cmdMax-3] + "..."
			}
			line := fmt.Sprintf("%s%-*s  %-8s  %s  %s",
				cursor, nameW, trunc(a.Name, nameW), statusLabel(st, code, appURL), process.FormatUsage(usage), cmd)
			b.WriteString(lineStyle.Render(line))
			b.WriteString("\n")
			if !compact && i == m.cursor {
				if a.Folder != "" {
					b.WriteString(mutedStyle.Render("    " + shortPath(a.Folder)))
					b.WriteString("\n")
				}
				if appURL != "" {
					b.WriteString(linkStyle.Render("    " + appURL + "  (o open)"))
					b.WriteString("\n")
				}
				if errMsg != "" && st == process.StatusFailed {
					for _, errLine := range strings.Split(errMsg, "\n") {
						b.WriteString(errorStyle.Render("    " + errLine))
						b.WriteString("\n")
					}
				}
			}
		}
	}

	if !compact {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("↑/↓ move  s start  d/x stop  r restart  o url  Ctrl+E folder  p profile  e edit  R reload  q quit"))
		b.WriteString("\n")
		if m.errFlash != "" {
			b.WriteString(errorStyle.Render(m.errFlash))
			b.WriteString("\n")
		} else if m.status != "" {
			b.WriteString(mutedStyle.Render(m.status))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func statusLabel(st process.Status, code int, appURL string) string {
	switch st {
	case process.StatusRunning:
		if port := process.PortFromURL(appURL); port > 0 {
			return statusRun.Render(fmt.Sprintf(":%d", port))
		}
		return statusRun.Render("running")
	case process.StatusRestarting:
		return statusRestart.Render("restarting")
	case process.StatusFailed:
		return statusFail.Render(fmt.Sprintf("fail(%d)", code))
	default:
		return statusStop.Render("stopped")
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var (
	titleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("79"))
	profileStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("79"))
	profileActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("79"))
	sectionStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Bold(true)
	mutedStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	itemStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	selectedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	helpStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	linkStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Underline(true)
	statusRun          = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	statusStop         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusFail         = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	statusRestart      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	editorTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	editorBorder       = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("240")).
				Padding(0, 1)
)
