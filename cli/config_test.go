package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := &App{Name: "blog", Image: "ghcr.io/me/blog", Domain: "blog.example.com", Port: 3000, Health: "/healthz", Replicas: 2}
	if err := orig.save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadApp(dir)
	if err != nil {
		t.Fatalf("loadApp: %v", err)
	}
	if *got != *orig {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", *got, *orig)
	}
}

func TestLoadAppDefaults(t *testing.T) {
	dir := t.TempDir()
	// Only the required fields; loadApp should supply port/health/replicas defaults.
	yaml := "name: api\nimage: ghcr.io/me/api\ndomain: api.example.com\n"
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := loadApp(dir)
	if err != nil {
		t.Fatalf("loadApp: %v", err)
	}
	if app.Port != 8080 || app.Health != "/health" || app.Replicas != 1 || app.Manual {
		t.Errorf("defaults not applied: %+v", app)
	}
}

func TestLoadAppComments(t *testing.T) {
	dir := t.TempDir()
	yaml := "# heading\nname: api\n\n# the image\nimage: ghcr.io/me/api\ndomain: api.example.com\nmanual: true\n"
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(yaml), 0o644)
	app, err := loadApp(dir)
	if err != nil {
		t.Fatalf("loadApp: %v", err)
	}
	if app.Name != "api" || !app.Manual {
		t.Errorf("comments/blank lines mishandled: %+v", app)
	}
}

func TestLoadAppMissingRequired(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("name: api\n"), 0o644)
	if _, err := loadApp(dir); err == nil {
		t.Error("expected error when image and domain are missing")
	}
}

func TestRenderGolden(t *testing.T) {
	dir := t.TempDir()
	app := &App{Name: "blog", Image: "ghcr.io/me/blog", Domain: "blog.example.com", Port: 8080, Health: "/health", Replicas: 1}
	if err := app.render(dir); err != nil {
		t.Fatalf("render: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "app-compose.golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (regenerate with UPDATE_GOLDEN=1 go test): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered compose does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderManualSkips(t *testing.T) {
	dir := t.TempDir()
	app := &App{Name: "x", Image: "img", Domain: "d.example.com", Port: 8080, Health: "/health", Replicas: 1, Manual: true}
	if err := app.render(dir); err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); !os.IsNotExist(err) {
		t.Error("manual: true must not write docker-compose.yml")
	}
}

func TestCurrentTagDefault(t *testing.T) {
	dir := t.TempDir()
	if tag := currentTag(dir); tag != "latest" {
		t.Errorf("missing .current_tag should default to latest, got %q", tag)
	}
	if err := setCurrentTag(dir, "abc123"); err != nil {
		t.Fatal(err)
	}
	if tag := currentTag(dir); tag != "abc123" {
		t.Errorf("currentTag = %q, want abc123", tag)
	}
}
