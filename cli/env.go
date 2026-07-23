package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const envUsage = `usage:
  platform env <name> list                  show keys and values in apps/<name>/.env
  platform env <name> set KEY=VALUE ...      add or update one or more keys
  platform env <name> unset KEY ...          remove one or more keys`

// cmdEnv manages apps/<name>/.env. Secrets live only here (chmod 600, gitignored).
func cmdEnv(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%s", envUsage)
	}
	name, sub := args[0], args[1]
	dir, err := appDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%s does not exist — run `platform new %s` first", dir, name)
	}
	switch sub {
	case "list":
		return envList(dir)
	case "set":
		return envSet(dir, name, args[2:])
	case "unset":
		return envUnset(dir, name, args[2:])
	default:
		return fmt.Errorf("unknown env subcommand %q\n%s", sub, envUsage)
	}
}

func envPath(dir string) string { return filepath.Join(dir, ".env") }

// readEnvLines returns the raw lines of .env (nil if the file is absent).
func readEnvLines(dir string) ([]string, error) {
	f, err := os.Open(envPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// writeEnvLines writes .env back with 0600 (secrets).
func writeEnvLines(dir string, lines []string) error {
	out := strings.Join(lines, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(envPath(dir), []byte(out), 0o600)
}

// envKey extracts the KEY from a "KEY=..." line, ignoring comments and blanks.
func envKey(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", false
	}
	k, _, ok := strings.Cut(t, "=")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(k), true
}

func envList(dir string) error {
	lines, err := readEnvLines(dir)
	if err != nil {
		return err
	}
	found := false
	for _, line := range lines {
		if k, ok := envKey(line); ok {
			// Print the assignment as-is (trimmed) so values are visible on the
			// operator's own box; access is SSH-only anyway.
			fmt.Println(strings.TrimSpace(line))
			_ = k
			found = true
		}
	}
	if !found {
		fmt.Println("(no variables set)")
	}
	return nil
}

func envSet(dir, name string, pairs []string) error {
	if len(pairs) == 0 {
		return fmt.Errorf("usage: platform env %s set KEY=VALUE ...", name)
	}
	updates := map[string]string{}
	var order []string
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return fmt.Errorf("%q is not KEY=VALUE", p)
		}
		k = strings.TrimSpace(k)
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("invalid key %q (must match [A-Za-z_][A-Za-z0-9_]*)", k)
		}
		if _, dup := updates[k]; !dup {
			order = append(order, k)
		}
		updates[k] = v
	}

	lines, err := readEnvLines(dir)
	if err != nil {
		return err
	}
	// Replace existing keys in place, preserving comments and ordering.
	for i, line := range lines {
		if k, ok := envKey(line); ok {
			if v, set := updates[k]; set {
				lines[i] = k + "=" + v
				delete(updates, k)
			}
		}
	}
	// Append keys that weren't already present, in the order given.
	for _, k := range order {
		if v, set := updates[k]; set {
			lines = append(lines, k+"="+v)
			delete(updates, k)
		}
	}
	if err := writeEnvLines(dir, lines); err != nil {
		return err
	}
	fmt.Printf("Updated %s (%d key(s)). Run `platform deploy %s` to apply — containers read .env at create time.\n",
		envPath(dir), len(order), name)
	return nil
}

func envUnset(dir, name string, keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("usage: platform env %s unset KEY ...", name)
	}
	remove := map[string]bool{}
	for _, k := range keys {
		remove[strings.TrimSpace(k)] = true
	}
	lines, err := readEnvLines(dir)
	if err != nil {
		return err
	}
	var kept []string
	removed := 0
	for _, line := range lines {
		if k, ok := envKey(line); ok && remove[k] {
			removed++
			continue
		}
		kept = append(kept, line)
	}
	if removed == 0 {
		return fmt.Errorf("none of those keys are set in %s", envPath(dir))
	}
	if err := writeEnvLines(dir, kept); err != nil {
		return err
	}
	fmt.Printf("Removed %d key(s) from %s. Run `platform deploy %s` to apply.\n", removed, envPath(dir), name)
	return nil
}
