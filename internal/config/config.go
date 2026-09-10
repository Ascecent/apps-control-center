package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultPath      = "apps.conf"
	DefaultProfile   = "default"
	UtilitiesProfile = "utilities"
)

// App is a named command from the config file.
type App struct {
	Name    string
	Folder  string // absolute path after ~ expansion; empty if unset
	Command string
}

// Profile is a named group of apps (excludes utilities).
type Profile struct {
	Name string
	Apps []App
}

// Config is the full parsed file.
type Config struct {
	Profiles  []Profile
	Utilities []App // always available; not selectable via profile switch
}

// ProfileNames returns selectable profile names (excludes utilities).
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for _, p := range c.Profiles {
		names = append(names, p.Name)
	}
	return names
}

// AppsFor returns the apps for the named selectable profile, or nil if not found.
func (c *Config) AppsFor(name string) []App {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p.Apps
		}
	}
	return nil
}

// HasProfile reports whether the config contains the named selectable profile.
func (c *Config) HasProfile(name string) bool {
	for _, p := range c.Profiles {
		if p.Name == name {
			return true
		}
	}
	return false
}

// Load parses apps.conf into a Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	return Parse(path, string(data))
}

// Parse parses config content.
// Format:
//
//	[utilities]          # always available across profiles
//	mailpit: ...
//
//	[profile]
//	name: folder | command
//
// Files without any [profile] header are treated as [default].
// App names in [utilities] must not appear in any other profile.
func Parse(label, content string) (*Config, error) {
	sc := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	var profiles []Profile
	var utilities []App
	var curName string
	seen := make(map[string]map[string]int) // section -> name -> line
	utilNames := make(map[string]int)       // utility name -> line

	ensureSection := func(name string) {
		if curName == name {
			return
		}
		curName = name
		if name == UtilitiesProfile {
			if seen[name] == nil {
				seen[name] = make(map[string]int)
			}
			return
		}
		for i := range profiles {
			if profiles[i].Name == name {
				return
			}
		}
		profiles = append(profiles, Profile{Name: name})
		seen[name] = make(map[string]int)
	}

	appendApp := func(app App, line int) error {
		if curName == UtilitiesProfile {
			if prev, dup := utilNames[app.Name]; dup {
				return fmt.Errorf("%s:%d: duplicate utility %q (also line %d)", label, line, app.Name, prev)
			}
			utilNames[app.Name] = line
			utilities = append(utilities, app)
			return nil
		}
		profSeen := seen[curName]
		if prev, dup := profSeen[app.Name]; dup {
			return fmt.Errorf("%s:%d: duplicate app %q in profile %q (also line %d)", label, line, app.Name, curName, prev)
		}
		profSeen[app.Name] = line
		for i := range profiles {
			if profiles[i].Name == curName {
				profiles[i].Apps = append(profiles[i].Apps, app)
				return nil
			}
		}
		return fmt.Errorf("%s:%d: internal: no profile %q", label, line, curName)
	}

	ensureSection(DefaultProfile)
	hasExplicitHeader := false

	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			if name == "" {
				return nil, fmt.Errorf("%s:%d: empty profile name", label, lineNum)
			}
			ensureSection(name)
			hasExplicitHeader = true
			continue
		}

		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected name: folder | command", label, lineNum)
		}
		name = strings.TrimSpace(name)
		rest = strings.TrimSpace(rest)
		if name == "" {
			return nil, fmt.Errorf("%s:%d: empty app name", label, lineNum)
		}
		if rest == "" {
			return nil, fmt.Errorf("%s:%d: empty definition", label, lineNum)
		}

		folder, command := splitFolderCommand(rest)
		if command == "" {
			return nil, fmt.Errorf("%s:%d: empty command", label, lineNum)
		}
		if folder != "" {
			expanded, err := expandPath(folder)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", label, lineNum, err)
			}
			folder = expanded
		}

		if err := appendApp(App{Name: name, Folder: folder, Command: command}, lineNum); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if hasExplicitHeader {
		out := profiles[:0]
		for _, p := range profiles {
			if p.Name == DefaultProfile && len(p.Apps) == 0 {
				continue
			}
			out = append(out, p)
		}
		profiles = out
	}

	// Utilities names must not collide with any selectable profile app.
	for _, p := range profiles {
		for _, a := range p.Apps {
			if utilLine, ok := utilNames[a.Name]; ok {
				return nil, fmt.Errorf("%s: app %q in profile %q conflicts with [utilities] (line %d)", label, a.Name, p.Name, utilLine)
			}
		}
	}

	if len(profiles) == 0 {
		profiles = []Profile{{Name: DefaultProfile}}
	}

	return &Config{Profiles: profiles, Utilities: utilities}, nil
}

// Save validates content, writes it to path, then returns the parsed Config.
func Save(path, content string) (*Config, error) {
	cfg, err := Parse(path, content)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(content, "\n") && content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	return cfg, nil
}

func splitFolderCommand(rest string) (folder, command string) {
	f, c, ok := strings.Cut(rest, "|")
	if !ok {
		return "", strings.TrimSpace(rest)
	}
	return strings.TrimSpace(f), strings.TrimSpace(c)
}

func expandPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		if p == "~" {
			p = home
		} else {
			p = filepath.Join(home, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}
