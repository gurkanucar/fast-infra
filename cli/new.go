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

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := app.save(dir); err != nil {
		return err
	}
	env := fmt.Sprintf(`# Secrets & config for %[1]s — never commit real values.
DATABASE_URL=postgres://postgres:CHANGE_ME@postgres:5432/%[1]s
REDIS_URL=redis://redis:6379
OTEL_EXPORTER_OTLP_ENDPOINT=http://openobserve:5080/api/default
`, name)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		return err
	}
	if err := app.render(dir); err != nil {
		return err
	}

	fmt.Printf(`
Created %s/
  app.yaml            app definition (edit and re-run deploy to apply)
  .env                secrets (chmod 600, gitignored)
  docker-compose.yml  generated — do not edit unless manual: true

Next:
  1. Create the database:  docker exec -it fast-infra-postgres-1 createdb -U postgres %s
  2. Point DNS %s -> this server
  3. Deploy:               platform deploy %s <tag>
`, dir, name, app.Domain, name)
	return nil
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
