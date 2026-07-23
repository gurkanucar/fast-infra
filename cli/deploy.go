package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func cmdDeploy(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: platform deploy <name> [tag]")
	}
	tag := "latest"
	if len(args) == 2 {
		tag = args[1]
	}
	return deploy(args[0], tag)
}

func cmdRollback(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: platform rollback <name>")
	}
	dir, err := appDir(args[0])
	if err != nil {
		return err
	}
	prev, ok := previousSuccess(dir, currentTag(dir))
	if !ok {
		return fmt.Errorf("no previous successful deployment found in history")
	}
	fmt.Printf("Rolling back %s to %s\n", args[0], prev)
	return deploy(args[0], prev)
}

func cmdScale(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: platform scale <name> <replicas>")
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 1 {
		return fmt.Errorf("replicas must be a positive integer")
	}
	dir, err := appDir(args[0])
	if err != nil {
		return err
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	app.Replicas = n
	if err := app.save(dir); err != nil {
		return err
	}
	tag := currentTag(dir)
	if err := dc(dir, tag, "up", "-d", "--no-recreate", "--scale", "app="+strconv.Itoa(n)); err != nil {
		return err
	}
	fmt.Printf("Scaled %s to %d replica(s) (tag %s)\n", args[0], n, tag)
	return nil
}

// deploy performs the rolling swap:
// render compose -> pull -> start new replicas alongside old -> wait healthy ->
// stop old -> settle scale -> record history + current tag.
func deploy(name, tag string) error {
	dir, err := appDir(name)
	if err != nil {
		return err
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	if err := app.render(dir); err != nil {
		return err
	}

	fmt.Printf("Deploying %s:%s (%d replica(s), one at a time)\n", app.Image, tag, app.Replicas)
	if err := dc(dir, tag, "pull", "app"); err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}

	// Sequential rolling: add one new replica, wait for it to pass its health
	// check, then drain one old replica — repeat until the old set is gone and
	// `replicas` new ones are up. Peak container count is replicas+1 rather than
	// 2×replicas, which matters on a small VPS (a Spring Boot app that needs
	// -Xmx256m no longer needs double that headroom to deploy). The Traefik
	// retry middleware keeps each brief swap window free of dropped requests.
	old := psQ(dir, tag) // containers running the previous image
	added, step := 0, 0
	for added < app.Replicas || len(old) > 0 {
		if added < app.Replicas {
			before := psQ(dir, tag)
			if err := dc(dir, tag, "up", "-d", "--no-recreate", "--scale", "app="+strconv.Itoa(len(before)+1)); err != nil {
				recordDeployment(dir, tag, "failed")
				return fmt.Errorf("starting new replica failed: %w", err)
			}
			newIDs := diff(psQ(dir, tag), before)
			if len(newIDs) == 0 {
				recordDeployment(dir, tag, "failed")
				return fmt.Errorf("no new container was created")
			}
			step++
			fmt.Printf("  [%d/%d] waiting for new replica to become healthy...\n", step, app.Replicas)
			if err := waitHealthy(newIDs, 120*time.Second); err != nil {
				fmt.Println("  health check failed — removing this replica and stopping the rollout.")
				for _, id := range newIDs {
					dockerOut("rm", "-f", id)
				}
				recordDeployment(dir, tag, "failed")
				return err
			}
			added++
		}
		if len(old) > 0 {
			id := old[0]
			old = old[1:]
			if _, err := dockerOut("stop", "-t", "30", id); err == nil {
				dockerOut("rm", id)
			}
		}
	}
	// Settle to exactly the desired count (no-op in the common case; also the
	// path for the very first deploy, where there were no old containers).
	if err := dc(dir, tag, "up", "-d", "--no-recreate", "--scale", "app="+strconv.Itoa(app.Replicas)); err != nil {
		return err
	}

	if err := setCurrentTag(dir, tag); err != nil {
		return err
	}
	if err := recordDeployment(dir, tag, "success"); err != nil {
		return err
	}
	fmt.Printf("Deployed %s:%s ✔\n", name, tag)
	return nil
}

func psQ(dir, tag string) []string {
	out, err := dcOut(dir, tag, "ps", "-q", "app")
	if err != nil {
		return nil
	}
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			ids = append(ids, l)
		}
	}
	return ids
}

func diff(all, old []string) []string {
	seen := map[string]bool{}
	for _, id := range old {
		seen[id] = true
	}
	var out []string
	for _, id := range all {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}

func waitHealthy(ids []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		healthy := 0
		for _, id := range ids {
			s, err := dockerOut("inspect", "-f", "{{.State.Health.Status}}", id)
			if err != nil {
				return fmt.Errorf("container %s disappeared during startup", id[:12])
			}
			switch strings.TrimSpace(s) {
			case "healthy":
				healthy++
			case "unhealthy":
				return fmt.Errorf("container %s is unhealthy (check its logs: docker logs %s)", id[:12], id[:12])
			}
		}
		if healthy == len(ids) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for containers to become healthy")
}
