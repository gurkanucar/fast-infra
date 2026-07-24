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
