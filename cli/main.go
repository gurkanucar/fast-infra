// platform — tiny deploy CLI for fast-infra.
// Zero external dependencies; shells out to the docker CLI.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const usage = `platform — deploy apps on a single VPS with zero downtime

Usage:
  platform new <name>                 scaffold apps/<name> (app.yaml, .env, compose)
  platform deploy <name> [tag]        rolling deploy (default tag: latest)
  platform rollback <name>            redeploy the previous successful tag
  platform scale <name> <replicas>    scale running replicas (persisted to app.yaml)
  platform status [name]              overview, or deployment history for one app

Run from the fast-infra repo root (the directory containing apps/).`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "deploy":
		err = cmdDeploy(os.Args[2:])
	case "rollback":
		err = cmdRollback(os.Args[2:])
	case "scale":
		err = cmdScale(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		fmt.Println(usage)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// appDir returns apps/<name>, verifying we run from the repo root.
func appDir(name string) (string, error) {
	if _, err := os.Stat("apps"); err != nil {
		return "", fmt.Errorf("apps/ not found — run from the fast-infra repo root")
	}
	return filepath.Join("apps", name), nil
}

// dc runs `docker compose` in dir with TAG set, streaming output.
func dc(dir, tag string, args ...string) error {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TAG="+tag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// dcOut runs `docker compose` in dir with TAG set, capturing stdout.
func dcOut(dir, tag string, args ...string) (string, error) {
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TAG="+tag)
	out, err := cmd.Output()
	return string(out), err
}

func dockerOut(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).Output()
	return string(out), err
}
