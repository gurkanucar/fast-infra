package main

import (
	"strings"
	"testing"
)

func TestCallerWorkflowAutoDeploy(t *testing.T) {
	wf := callerWorkflow("acme", "blog", "main", true, nil)
	for _, want := range []string{
		"name: deploy-blog",
		"  push:\n    branches: [main]",
		"  workflow_dispatch:",
		"packages: write",
		"app_name: blog",
		"uses: acme/fast-infra/.github/workflows/deploy-template.yml@master",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("auto-deploy workflow missing %q:\n%s", want, wf)
		}
	}
	if strings.Contains(wf, "paths:") {
		t.Errorf("no path filter was requested, but the workflow has one:\n%s", wf)
	}
}

func TestCallerWorkflowNoAutoDeploy(t *testing.T) {
	wf := callerWorkflow("acme", "blog", "main", false, nil)
	if strings.Contains(wf, "push:") {
		t.Errorf("auto-deploy off must omit the push trigger:\n%s", wf)
	}
	if !strings.Contains(wf, "workflow_dispatch:") {
		t.Errorf("manual dispatch must still be available:\n%s", wf)
	}
}

func TestParseCallerTriggersRoundTrip(t *testing.T) {
	// Auto-deploy with a path filter round-trips through parseCallerTriggers.
	wf := callerWorkflow("acme", "api", "release", true, []string{"api/**", "Dockerfile"})
	auto, branch, paths := parseCallerTriggers(wf)
	if !auto || branch != "release" {
		t.Fatalf("auto=%v branch=%q, want true/release", auto, branch)
	}
	if len(paths) != 2 || paths[0] != "api/**" || paths[1] != "Dockerfile" {
		t.Fatalf("paths=%v, want [api/** Dockerfile]", paths)
	}

	// Auto-deploy off means no push trigger, so no branch is parsed back.
	auto, _, _ = parseCallerTriggers(callerWorkflow("acme", "api", "main", false, nil))
	if auto {
		t.Error("auto-deploy off must parse back as false")
	}
}

func TestCallerWorkflowPaths(t *testing.T) {
	wf := callerWorkflow("acme", "api", "release", true, []string{"api/**", "Dockerfile"})
	for _, want := range []string{
		"branches: [release]",
		"    paths:",
		`      - "api/**"`,
		`      - "Dockerfile"`,
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("path-filtered workflow missing %q:\n%s", want, wf)
		}
	}
}
