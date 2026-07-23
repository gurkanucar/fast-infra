package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdRemove(args []string) error {
	keepFiles := false
	name := ""
	for _, a := range args {
		switch {
		case a == "--keep-files":
			keepFiles = true
		case name == "":
			name = a
		default:
			return fmt.Errorf("usage: platform remove <name> [--keep-files]")
		}
	}
	if name == "" {
		return fmt.Errorf("usage: platform remove <name> [--keep-files]")
	}
	dir, err := appDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("%s does not exist", dir)
	}

	action := "stop its containers and delete " + dir
	if keepFiles {
		action = "stop its containers (files kept)"
	}
	fmt.Printf("This will %s.\nThe Postgres database and any pushed images are left untouched.\nRemove %q? [y/N]: ", action, name)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		fmt.Println("Aborted.")
		return nil
	}

	// Stop and remove the app's containers (best-effort — it may never have
	// been deployed, or already be down).
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		if err := dc(dir, currentTag(dir), "down"); err != nil {
			fmt.Fprintln(os.Stderr, "warning: docker compose down reported:", err)
		}
	}

	if keepFiles {
		fmt.Printf("Stopped %s. Files kept in %s — run `platform deploy %s` to bring it back.\n", name, dir, name)
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Printf("Removed %s.\n", name)
	fmt.Printf("Its Postgres database is left intact — drop it yourself if you want it gone:\n  docker exec -it fast-infra-postgres-1 dropdb -U postgres %s\n", name)
	fmt.Println("Images already pushed to the registry (GHCR) are also left as-is.")
	return nil
}
