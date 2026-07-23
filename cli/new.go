package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func cmdNew(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: platform new <name>")
	}
	name := args[0]
	if !nameRe.MatchString(name) {
		return fmt.Errorf("name must be lowercase letters, digits and dashes")
	}
	dir, err := appDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}

	rd := bufio.NewReader(os.Stdin)
	app := &App{Name: name, Health: "/health", Replicas: 1}
	app.Image = ask(rd, "Image (e.g. ghcr.io/you/"+name+", no tag)", "")
	app.Domain = ask(rd, "Domain", name+".example.com")
	app.Port, _ = strconv.Atoi(ask(rd, "Container port", "8080"))
	app.Health = ask(rd, "Health path", "/health")
	wantDB := askYN(rd, "Create a dedicated Postgres database + user?")
	wantRedis := askYN(rd, "Create a Redis user scoped to "+name+":*?")

	outcome, err := createApp(app, wantDB, wantRedis)
	if err != nil {
		return err
	}
	if outcome.DBCreated {
		fmt.Printf("Created Postgres database and role %q.\n", name)
	}
	if outcome.RedisCreated {
		fmt.Printf("Created Redis user %q scoped to %s:*\n", name, name)
	}
	for _, w := range outcome.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}

	fmt.Printf(`
Created %s/
  app.yaml            app definition (edit and re-run deploy to apply)
  .env                secrets (chmod 600, gitignored)
  docker-compose.yml  generated — do not edit unless manual: true

Next:
`, dir)
	if !outcome.DBCreated {
		fmt.Printf("  - Create the database: docker exec -it %s createdb -U postgres %s\n", pgContainer, name)
	}
	fmt.Printf("  - Point DNS %s -> this server\n", app.Domain)
	fmt.Printf("  - Deploy:              platform deploy %s <tag>\n", name)
	return nil
}

// provisionOutcome reports what createApp provisioned and any non-fatal
// warnings (a failed provision leaves the manual .env template in place).
type provisionOutcome struct {
	DBCreated    bool
	RedisCreated bool
	Warnings     []string
}

// createApp scaffolds apps/<name>: app.yaml, optional Postgres/Redis
// provisioning, .env, and the rendered compose file. Shared by the CLI (`new`)
// and the web panel so both take exactly one path.
func createApp(app *App, wantDB, wantRedis bool) (provisionOutcome, error) {
	var out provisionOutcome
	if !nameRe.MatchString(app.Name) {
		return out, fmt.Errorf("name must be lowercase letters, digits and dashes")
	}
	if app.Image == "" || app.Domain == "" {
		return out, fmt.Errorf("image and domain are required")
	}
	if app.Port == 0 {
		app.Port = 8080
	}
	if app.Health == "" {
		app.Health = "/health"
	}
	if app.Replicas == 0 {
		app.Replicas = 1
	}
	dir, err := appDir(app.Name)
	if err != nil {
		return out, err
	}
	if _, err := os.Stat(dir); err == nil {
		return out, fmt.Errorf("%s already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return out, err
	}
	if err := app.save(dir); err != nil {
		return out, err
	}

	var dbPass, redisPass *string
	if wantDB {
		if pw, err := provisionPostgres(app.Name); err != nil {
			out.Warnings = append(out.Warnings, "Postgres provisioning failed: "+err.Error())
		} else {
			dbPass, out.DBCreated = &pw, true
		}
	}
	if wantRedis {
		if pw, err := provisionRedis(app.Name); err != nil {
			out.Warnings = append(out.Warnings, "Redis provisioning failed: "+err.Error())
		} else {
			redisPass, out.RedisCreated = &pw, true
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(renderAppEnv(app.Name, dbPass, redisPass)), 0o600); err != nil {
		return out, err
	}
	return out, app.render(dir)
}

// askYN prompts for a yes/no answer, defaulting to no.
func askYN(rd *bufio.Reader, prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	line, _ := rd.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// renderAppEnv builds the app's .env. A non-nil password means that resource was
// provisioned and gets real credentials; nil keeps the manual template.
func renderAppEnv(name string, dbPass, redisPass *string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Secrets & config for %s — never commit real values.\n", name)
	if dbPass != nil {
		fmt.Fprintf(&b, "DATABASE_URL=postgres://%[1]s:%[2]s@postgres:5432/%[1]s\n", name, *dbPass)
	} else {
		fmt.Fprintf(&b, "DATABASE_URL=postgres://postgres:CHANGE_ME@postgres:5432/%s\n", name)
	}
	if redisPass != nil {
		fmt.Fprintf(&b, "REDIS_URL=redis://%s:%s@redis:6379\n", name, *redisPass)
	} else {
		b.WriteString("REDIS_URL=redis://redis:6379\n")
	}
	b.WriteString("OTEL_EXPORTER_OTLP_ENDPOINT=http://openobserve:5080/api/default\n")
	return b.String()
}

func ask(rd *bufio.Reader, prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, _ := rd.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}
