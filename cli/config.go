package main

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/app-compose.yml.tmpl
var tmplFS embed.FS

// App is the flat app.yaml schema. Secrets live in .env, not here.
type App struct {
	Name     string
	Image    string // without tag
	Domain   string
	Port     int
	Health   string
	Replicas int
	Manual   bool // if true, platform never rewrites docker-compose.yml
}

// loadApp parses apps/<name>/app.yaml (flat "key: value" lines).
func loadApp(dir string) (*App, error) {
	f, err := os.Open(filepath.Join(dir, "app.yaml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	app := &App{Health: "/health", Replicas: 1, Port: 8080}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "name":
			app.Name = v
		case "image":
			app.Image = v
		case "domain":
			app.Domain = v
		case "port":
			app.Port, err = strconv.Atoi(v)
		case "health":
			app.Health = v
		case "replicas":
			app.Replicas, err = strconv.Atoi(v)
		case "manual":
			app.Manual = v == "true"
		}
		if err != nil {
			return nil, fmt.Errorf("app.yaml: bad value for %q: %v", k, err)
		}
	}
	if app.Name == "" || app.Image == "" || app.Domain == "" {
		return nil, fmt.Errorf("app.yaml must set name, image and domain")
	}
	return app, nil
}

func (a *App) save(dir string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", a.Name)
	fmt.Fprintf(&b, "image: %s\n", a.Image)
	fmt.Fprintf(&b, "domain: %s\n", a.Domain)
	fmt.Fprintf(&b, "port: %d\n", a.Port)
	fmt.Fprintf(&b, "health: %s\n", a.Health)
	fmt.Fprintf(&b, "replicas: %d\n", a.Replicas)
	if a.Manual {
		b.WriteString("manual: true\n")
	}
	return os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(b.String()), 0o644)
}

// render writes docker-compose.yml from the embedded template (unless manual).
func (a *App) render(dir string) error {
	if a.Manual {
		return nil
	}
	t, err := template.ParseFS(tmplFS, "templates/app-compose.yml.tmpl")
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, a)
}

// currentTag reads/writes apps/<name>/.current_tag.
func currentTag(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".current_tag"))
	if err != nil {
		return "latest"
	}
	return strings.TrimSpace(string(b))
}

func setCurrentTag(dir, tag string) error {
	return os.WriteFile(filepath.Join(dir, ".current_tag"), []byte(tag+"\n"), 0o644)
}
