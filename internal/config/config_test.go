package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlatFileParsedAsDefault(t *testing.T) {
	cfg, err := Parse("test", "api: /tmp | php artisan serve\nweb: npm run dev\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != DefaultProfile {
		t.Fatalf("expected single default profile, got %+v", cfg.ProfileNames())
	}
	if len(cfg.Utilities) != 0 {
		t.Fatalf("expected no utilities, got %d", len(cfg.Utilities))
	}
	apps := cfg.AppsFor(DefaultProfile)
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}
}

func TestProfileHeaders(t *testing.T) {
	content := `
[work]
api: /tmp/api | php artisan serve
web: /tmp/web | npm run dev

[personal]
blog: npm run dev
`
	cfg, err := Parse("test", content)
	if err != nil {
		t.Fatal(err)
	}
	names := cfg.ProfileNames()
	if len(names) != 2 || names[0] != "work" || names[1] != "personal" {
		t.Fatalf("profiles = %v", names)
	}
}

func TestUtilities(t *testing.T) {
	content := `
[utilities]
mailpit: /tmp | ./mailpit
redis: redis-server

[work]
api: /tmp/api | php artisan serve

[personal]
blog: npm run dev
`
	cfg, err := Parse("test", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Utilities) != 2 {
		t.Fatalf("utilities = %d", len(cfg.Utilities))
	}
	if cfg.Utilities[0].Name != "mailpit" || cfg.Utilities[0].Command != "./mailpit" {
		t.Fatalf("util[0] = %+v", cfg.Utilities[0])
	}
	names := cfg.ProfileNames()
	for _, n := range names {
		if n == UtilitiesProfile {
			t.Fatal("utilities must not appear in ProfileNames")
		}
	}
	if cfg.HasProfile(UtilitiesProfile) {
		t.Fatal("HasProfile(utilities) should be false")
	}
	if len(names) != 2 {
		t.Fatalf("selectable profiles = %v", names)
	}
}

func TestUtilitiesNameConflict(t *testing.T) {
	_, err := Parse("test", "[utilities]\napi: a\n\n[work]\napi: b\n")
	if err == nil {
		t.Fatal("expected conflict error for same key")
	}
}

func TestDuplicateWithinProject(t *testing.T) {
	_, err := Parse("test", "[work]\n@api: /tmp\nserve: a\nserve: b\n")
	if err == nil {
		t.Fatal("expected duplicate error within project")
	}
}

func TestSameName_DifferentProjects(t *testing.T) {
	cfg, err := Parse("test", `
[work]
@api: /tmp/api
backend: php artisan serve

@web: /tmp/web
backend: npm run dev
`)
	if err != nil {
		t.Fatal(err)
	}
	apps := cfg.AppsFor("work")
	if len(apps) != 2 {
		t.Fatalf("got %d apps", len(apps))
	}
	if apps[0].Key() != "api/backend" || apps[1].Key() != "web/backend" {
		t.Fatalf("keys = %q, %q", apps[0].Key(), apps[1].Key())
	}
}

func TestDuplicateUngroupedWithinProfile(t *testing.T) {
	_, err := Parse("test", "[work]\napi: a\napi: b\n")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestSameName_DifferentProfiles(t *testing.T) {
	cfg, err := Parse("test", "[work]\napi: /tmp | php artisan serve\n\n[personal]\napi: npm start\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(cfg.Profiles))
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse("test", "api: ~/myapp | php artisan serve\n")
	if err != nil {
		t.Fatal(err)
	}
	apps := cfg.AppsFor(DefaultProfile)
	want := filepath.Join(home, "myapp")
	if apps[0].Folder != want {
		t.Fatalf("folder = %q, want %q", apps[0].Folder, want)
	}
	if !strings.HasSuffix(apps[0].Folder, "myapp") {
		t.Fatal("expected expanded path")
	}
}

func TestNamedProjectGroup(t *testing.T) {
	dir := t.TempDir()
	content := `
[work]
@api: ` + dir + `
serve: php artisan serve
queue: php artisan queue:work
web: /tmp/web | npm run dev
`
	cfg, err := Parse("test", content)
	if err != nil {
		t.Fatal(err)
	}
	apps := cfg.AppsFor("work")
	if len(apps) != 3 {
		t.Fatalf("got %d apps: %+v", len(apps), apps)
	}
	if apps[0].Name != "serve" || apps[0].Project != "api" || apps[0].Folder != dir {
		t.Fatalf("serve = %+v", apps[0])
	}
	if apps[1].Name != "queue" || apps[1].Project != "api" || apps[1].Folder != dir {
		t.Fatalf("queue = %+v", apps[1])
	}
	if apps[2].Name != "web" || apps[2].Project != "" || apps[2].Command != "npm run dev" {
		t.Fatalf("web = %+v", apps[2])
	}
	absWeb, _ := filepath.Abs("/tmp/web")
	if apps[2].Folder != absWeb {
		t.Fatalf("web folder = %q, want %q", apps[2].Folder, absWeb)
	}
}

func TestBareFolderGroupUsesBasename(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse("test", "@~/myapp\nserve: npm start\n")
	if err != nil {
		t.Fatal(err)
	}
	apps := cfg.AppsFor(DefaultProfile)
	want := filepath.Join(home, "myapp")
	if apps[0].Folder != want || apps[0].Project != "myapp" || apps[0].Command != "npm start" {
		t.Fatalf("got %+v, want folder %q project myapp", apps[0], want)
	}
}

func TestSaveValidatesThenWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps.conf")

	if _, err := Save(path, "bad line without colon\n"); err == nil {
		t.Fatal("expected validation error")
	}

	cfg, err := Save(path, "[utilities]\nmailpit: /tmp | ./mailpit\n\n[work]\napi: /tmp | php artisan serve\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Utilities) != 1 || !cfg.HasProfile("work") {
		t.Fatalf("unexpected cfg: utils=%d profiles=%v", len(cfg.Utilities), cfg.ProfileNames())
	}
}
