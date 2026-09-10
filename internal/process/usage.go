package process

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Usage is a CPU/memory snapshot for a process or process group.
type Usage struct {
	CPUPercent float64
	RSSBytes   uint64
	OK         bool
}

type cpuSample struct {
	ticks uint64
	at    time.Time
}

// Linux default; almost all distros use 100.
const clkTck = 100.0

var (
	selfSample  sync.Mutex
	lastSelf    cpuSample
	lastSelfCPU float64
)

// SelfUsage returns CPU% and RSS for this process (the TUI).
func SelfUsage() Usage {
	pid := os.Getpid()
	ticks, rss, err := readProcStats(pid)
	if err != nil {
		return Usage{}
	}
	now := time.Now()

	selfSample.Lock()
	defer selfSample.Unlock()
	cpu := lastSelfCPU
	if !lastSelf.at.IsZero() {
		elapsed := now.Sub(lastSelf.at).Seconds()
		if elapsed > 0 && ticks >= lastSelf.ticks {
			cpu = (float64(ticks-lastSelf.ticks) / clkTck) / elapsed * 100
			lastSelfCPU = cpu
		}
	}
	lastSelf = cpuSample{ticks: ticks, at: now}
	return Usage{CPUPercent: cpu, RSSBytes: rss, OK: true}
}

func (m *Manager) sampleUsage(p *proc) Usage {
	if p.cmd == nil || p.cmd.Process == nil || p.status != StatusRunning {
		p.lastCPU = cpuSample{}
		p.lastCPUPct = 0
		return Usage{}
	}
	pgid := p.cmd.Process.Pid
	ticks, rss, err := readGroupStats(pgid)
	if err != nil {
		return Usage{}
	}
	now := time.Now()
	cpu := p.lastCPUPct
	if !p.lastCPU.at.IsZero() {
		elapsed := now.Sub(p.lastCPU.at).Seconds()
		if elapsed > 0 && ticks >= p.lastCPU.ticks {
			cpu = (float64(ticks-p.lastCPU.ticks) / clkTck) / elapsed * 100
			p.lastCPUPct = cpu
		}
	}
	p.lastCPU = cpuSample{ticks: ticks, at: now}
	return Usage{CPUPercent: cpu, RSSBytes: rss, OK: true}
}

// Usage returns CPU%/RSS for the app's process group.
func (m *Manager) Usage(name string) Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[name]
	if !ok {
		return Usage{}
	}
	return m.sampleUsage(p)
}

func readGroupStats(pgid int) (ticks uint64, rss uint64, err error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0, err
	}
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		pgrp, t, r, readErr := readProcPGRP(pid)
		if readErr != nil {
			continue
		}
		if pgrp != pgid {
			continue
		}
		found = true
		ticks += t
		rss += r
	}
	if !found {
		t, r, readErr := readProcStats(pgid)
		if readErr != nil {
			return 0, 0, readErr
		}
		return t, r, nil
	}
	return ticks, rss, nil
}

func readProcStats(pid int) (ticks uint64, rss uint64, err error) {
	_, ticks, rss, err = readProcPGRP(pid)
	return ticks, rss, err
}

func readProcPGRP(pid int) (pgrp int, ticks uint64, rss uint64, err error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, 0, 0, err
	}
	return parseStat(string(data))
}

func parseStat(s string) (pgrp int, ticks uint64, rss uint64, err error) {
	start := strings.IndexByte(s, '(')
	end := strings.LastIndexByte(s, ')')
	if start < 0 || end < 0 || end+2 >= len(s) {
		return 0, 0, 0, fmt.Errorf("invalid stat")
	}
	rest := strings.Fields(s[end+2:])
	// After comm: state(0) ppid(1) pgrp(2) ... utime(11) stime(12) ... rss(21)
	if len(rest) < 22 {
		return 0, 0, 0, fmt.Errorf("short stat")
	}
	pgrp, err = strconv.Atoi(rest[2])
	if err != nil {
		return 0, 0, 0, err
	}
	utime, err := strconv.ParseUint(rest[11], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	stime, err := strconv.ParseUint(rest[12], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	rssPages, err := strconv.ParseUint(rest[21], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	pageSize := uint64(os.Getpagesize())
	return pgrp, utime + stime, rssPages * pageSize, nil
}

// FormatUsage renders CPU and RSS for the TUI, or placeholders when unavailable.
func FormatUsage(u Usage) string {
	if !u.OK {
		return "  —      —"
	}
	return fmt.Sprintf("%5.1f%% %6s", u.CPUPercent, formatBytes(u.RSSBytes))
}

func formatBytes(n uint64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fG", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fM", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.0fK", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
