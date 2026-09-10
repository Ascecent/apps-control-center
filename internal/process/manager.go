package process

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Status int

const (
	StatusStopped Status = iota
	StatusRunning
	StatusFailed
	StatusRestarting
)

func (s Status) String() string {
	switch s {
	case StatusStopped:
		return "stopped"
	case StatusRunning:
		return "running"
	case StatusFailed:
		return "failed"
	case StatusRestarting:
		return "restarting"
	default:
		return "unknown"
	}
}

const maxOutputBytes = 32 * 1024

// ExitEvent is emitted when a managed process exits on its own.
type ExitEvent struct {
	Name     string
	ExitCode int
	Err      error
	Output   string
}

// cappedBuffer keeps the most recent bytes written (stdout/stderr).
type cappedBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

func newCappedBuffer(n int) *cappedBuffer {
	return &cappedBuffer{cap: n}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(p) >= c.cap {
		c.buf = append([]byte(nil), p[len(p)-c.cap:]...)
		return len(p), nil
	}
	need := len(c.buf) + len(p) - c.cap
	if need > 0 {
		c.buf = c.buf[need:]
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(bytes.Clone(c.buf))
}

type proc struct {
	cmd        *exec.Cmd
	command    string
	folder     string
	status     Status
	exitCode   int
	errMsg     string
	output     *cappedBuffer
	done       chan struct{}
	lastCPU    cpuSample
	lastCPUPct float64
}

// Manager tracks process groups started by the TUI.
type Manager struct {
	mu       sync.Mutex
	procs    map[string]*proc
	onExit   func(ExitEvent)
	stopWait time.Duration
}

func New(onExit func(ExitEvent)) *Manager {
	return &Manager{
		procs:    make(map[string]*proc),
		onExit:   onExit,
		stopWait: 3 * time.Second,
	}
}

func (m *Manager) Ensure(name, folder, command string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.procs[name]; ok {
		p.command = command
		p.folder = folder
		return
	}
	m.procs[name] = &proc{command: command, folder: folder, status: StatusStopped}
}

func (m *Manager) Remove(name string) {
	_ = m.Stop(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.procs, name)
}

func (m *Manager) Command(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.procs[name]; ok {
		return p.command
	}
	return ""
}

func (m *Manager) Status(name string) (Status, int, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[name]
	if !ok {
		return StatusStopped, 0, ""
	}
	return p.status, p.exitCode, p.errMsg
}

// URL returns a browsable local URL detected from the app's output, if any.
func (m *Manager) URL(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[name]
	if !ok || p.status != StatusRunning || p.output == nil {
		return ""
	}
	return DetectURL(p.output.String())
}

func (m *Manager) Start(name string) error {
	m.mu.Lock()
	p, ok := m.procs[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	if p.status == StatusRunning || p.status == StatusRestarting {
		m.mu.Unlock()
		return nil
	}
	out := newCappedBuffer(maxOutputBytes)
	cmd := exec.Command("sh", "-c", p.command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = out
	cmd.Stderr = out
	if p.folder != "" {
		cmd.Dir = p.folder
	}
	if err := cmd.Start(); err != nil {
		p.status = StatusFailed
		p.errMsg = err.Error()
		p.exitCode = -1
		m.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	p.cmd = cmd
	p.output = out
	p.done = done
	p.status = StatusRunning
	p.errMsg = ""
	p.exitCode = 0
	p.lastCPU = cpuSample{}
	p.lastCPUPct = 0
	m.mu.Unlock()

	go m.wait(name, cmd, out, done)
	return nil
}

func (m *Manager) wait(name string, cmd *exec.Cmd, out *cappedBuffer, done chan struct{}) {
	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	captured := strings.TrimSpace(out.String())

	m.mu.Lock()
	p, ok := m.procs[name]
	notify := false
	var event ExitEvent
	if ok && p.cmd == cmd {
		intentional := p.status == StatusStopped || p.status == StatusRestarting
		if !intentional {
			if err != nil {
				p.status = StatusFailed
				p.errMsg = failureMessage(err, exitCode, captured)
			} else {
				p.status = StatusStopped
				p.errMsg = ""
			}
			p.exitCode = exitCode
			notify = true
			event = ExitEvent{Name: name, ExitCode: exitCode, Err: err, Output: captured}
		}
		p.cmd = nil
		p.output = nil
	}
	cb := m.onExit
	m.mu.Unlock()
	close(done)

	if notify && cb != nil {
		cb(event)
	}
}

func failureMessage(err error, exitCode int, output string) string {
	reason := lastNonEmptyLines(output, 8)
	if reason == "" {
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("exit status %d", exitCode)
	}
	return reason
}

func lastNonEmptyLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) >= n {
			break
		}
	}
	if len(kept) == 0 {
		return ""
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, "\n")
}

func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	p, ok := m.procs[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	cmd := p.cmd
	done := p.done
	if cmd == nil || cmd.Process == nil {
		p.status = StatusStopped
		m.mu.Unlock()
		return nil
	}
	pgid := cmd.Process.Pid
	wasRestarting := p.status == StatusRestarting
	if !wasRestarting {
		p.status = StatusStopped
	}
	wait := m.stopWait
	m.mu.Unlock()

	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done
	}
	return nil
}

func (m *Manager) Restart(name string) error {
	m.mu.Lock()
	p, ok := m.procs[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", name)
	}
	running := p.cmd != nil && p.cmd.Process != nil
	p.status = StatusRestarting
	m.mu.Unlock()

	if running {
		if err := m.Stop(name); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if p, ok := m.procs[name]; ok {
		p.status = StatusStopped
	}
	m.mu.Unlock()
	return m.Start(name)
}

// StopAll stops every managed process.
func (m *Manager) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.procs))
	for name := range m.procs {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		_ = m.Stop(name)
	}
}
