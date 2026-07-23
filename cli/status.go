package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdStatus(args []string) error {
	if len(args) == 1 {
		return statusOne(args[0])
	}
	if _, err := os.Stat("apps"); err != nil {
		return fmt.Errorf("apps/ not found — run from the fast-infra repo root")
	}
	entries, err := os.ReadDir("apps")
	if err != nil {
		return err
	}
	fmt.Printf("%-16s %-8s %-14s %-10s %s\n", "APP", "STATE", "TAG", "REPLICAS", "DOMAIN")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join("apps", e.Name())
		app, err := loadApp(dir)
		if err != nil {
			continue
		}
		tag := currentTag(dir)
		running, healthy := replicaState(dir, tag)
		state := "✗ down"
		if running > 0 {
			state = "✓ up"
			if healthy < running {
				state = "! degraded"
			}
		}
		fmt.Printf("%-16s %-8s %-14s %d/%d        %s\n",
			app.Name, state, tag, healthy, app.Replicas, app.Domain)
	}
	return nil
}

func statusOne(name string) error {
	dir, err := appDir(name)
	if err != nil {
		return err
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	tag := currentTag(dir)
	running, healthy := replicaState(dir, tag)
	fmt.Printf("%s\n  image:    %s:%s\n  domain:   https://%s\n  replicas: %d healthy / %d running (want %d)\n\n",
		app.Name, app.Image, tag, app.Domain, healthy, running, app.Replicas)

	hist, _ := readHistory(dir)
	if len(hist) == 0 {
		fmt.Println("  no deployments yet")
		return nil
	}
	fmt.Println("  deployments (newest first):")
	for i := len(hist) - 1; i >= 0 && i >= len(hist)-10; i-- {
		d := hist[i]
		mark := "✓"
		if d.Status != "success" {
			mark = "✗"
		}
		cur := ""
		if d.Tag == tag && d.Status == "success" {
			cur = "  <- current"
		}
		fmt.Printf("  %s %-14s %s%s\n", mark, d.Tag, d.Time.Format("2006-01-02 15:04"), cur)
	}
	return nil
}

func replicaState(dir, tag string) (running, healthy int) {
	for _, id := range psQ(dir, tag) {
		s, err := dockerOut("inspect", "-f", "{{.State.Status}} {{.State.Health.Status}}", id)
		if err != nil {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(s))
		if len(parts) > 0 && parts[0] == "running" {
			running++
			if len(parts) > 1 && parts[1] == "healthy" {
				healthy++
			}
		}
	}
	return
}
