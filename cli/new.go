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

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := app.save(dir); err != nil {
		return err
	}

	// Optional provisioning (opt-in). Best-effort: on failure keep going with
	// the manual template so the app scaffold is never left half-created.
	var dbPass, redisPass *string
	if wantDB {
		if pw, err := provisionPostgres(name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: Postgres provisioning failed (%v)\n  create it by hand: docker exec -it %s createdb -U postgres %s\n", err, pgContainer, name)
		} else {
			dbPass = &pw
			fmt.Printf("Created Postgres database and role %q.\n", name)
		}
	}
	if wantRedis {
		if pw, err := provisionRedis(name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: Redis provisioning failed (%v)\n", err)
		} else {
			redisPass = &pw
			fmt.Printf("Created Redis user %q scoped to %s:*\n", name, name)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(renderAppEnv(name, dbPass, redisPass)), 0o600); err != nil {
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
`, dir)
	if dbPass == nil {
		fmt.Printf("  - Create the database: docker exec -it %s createdb -U postgres %s\n", pgContainer, name)
	}
	fmt.Printf("  - Point DNS %s -> this server\n", app.Domain)
	fmt.Printf("  - Deploy:              platform deploy %s <tag>\n", name)
	return nil
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
