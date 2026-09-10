# Apps Control Center

A terminal UI for starting, stopping, and managing local app processes from a simple config file.

Built for day-to-day work with Laravel, Vite/npm, and similar stacks: switch between **work** and **personal** profiles, keep shared tools (Mailpit, Redis, …) running in **`[utilities]`**, edit config live, open project folders in your IDE, and see ports, URLs, CPU, and memory at a glance.

## Features

- Start / stop / restart apps (process-group aware)
- **Profiles** — separate app sets (e.g. work vs personal)
- **`[utilities]`** — shared apps always available; survive profile switches
- Live config reload (file watch + in-TUI editor)
- Detect listen URLs/ports from process output; open in browser
- Open project folders in VS Code, Cursor, PHPStorm, Vim, …
- Per-app and TUI CPU / RSS usage

## Requirements

- Go **1.24+**
- Linux (uses `/proc` for resource usage and process groups)

## Install

```bash
git clone https://github.com/ascecent/apps-control-center.git
cd apps-control-center
go build -o bin/acc ./cmd/acc
```

Or run without installing:

```bash
go run ./cmd/acc
```

## Quick start

```bash
cp apps.conf.example apps.conf
# edit apps.conf with your folders and commands
./bin/acc
```

The TUI reads **`./apps.conf`** in the current working directory (no other path).

## Config format

```ini
# Shared tools — always listed; keep running when you switch profiles
[utilities]
mailpit: ~/tools/mailpit | ./mailpit

[work]
api: ~/work/api | php artisan serve
web: ~/work/web | npm run dev

[personal]
blog: ~/personal/blog | npm run dev
```

| Piece | Meaning |
|--------|---------|
| `[name]` | Profile header. `[utilities]` is reserved for shared apps. |
| `app: folder \| command` | Run `command` with `folder` as cwd (`~` expanded). |
| `app: command` | Legacy form — command only, no folder / IDE open. |

Notes:

- Blank lines and `#` comments are ignored.
- App names may repeat across selectable profiles; they **must not** collide with `[utilities]` names.
- Files without any `[profile]` header load as a single `[default]` profile.

See `apps.conf.example` for a full sample.

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` (or `k` / `j`) | Move selection |
| `s` | Start |
| `d` / `x` | Stop |
| `r` | Restart |
| `o` | Open detected URL in the browser |
| `Ctrl+E` | Open selected app’s folder in an editor (picker) |
| `p` | Switch profile (utilities keep running) |
| `e` | Edit `apps.conf` in the TUI |
| `Ctrl+S` | Save & apply (in editor) |
| `Esc` | Cancel editor / picker |
| `R` | Reload config from disk |
| `q` | Quit (stops all managed processes) |

## How it works (briefly)

- Commands run via `sh -c` in their own process group; stop sends `SIGTERM` (then `SIGKILL` if needed).
- Config changes on disk are watched and applied automatically (paused while the in-TUI editor is open).
- URLs/ports are scraped from captured stdout/stderr (Laravel, Vite, etc.).
- Editors are discovered from `PATH` (code, cursor, phpstorm, nvim, …).

## Development

```bash
go test ./...
go build -o bin/acc ./cmd/acc
```

## Disclaimer — MADE WITH AI

**This project was made with AI.** Substantial parts of the design and implementation were produced with AI coding assistants, then reviewed and iterated on by a human. Use at your own risk; treat it as a personal tooling project, not production-hardened infrastructure.

## License

No license file is included yet. If you fork or redistribute, add one that matches how you want the project to be used.
