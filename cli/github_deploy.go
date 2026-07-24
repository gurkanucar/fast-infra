package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/ssh"
)

// ghPut sends a PUT with a JSON body to the GitHub API.
func ghPut(path string, body []byte) error {
	req, err := http.NewRequest("PUT", "https://api.github.com"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ghToken())
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := ghClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// --- SSH deploy key ---------------------------------------------------------

func ghDeployKeyPath() string { return filepath.Join("infra", ".gh_deploy_key") }

// authKeysPath is the host's authorized_keys, mounted into the panel container.
const authKeysPath = "/root/.ssh/authorized_keys"

// ensureDeployKey returns the deploy private key, generating one (and installing
// its public half on the VPS) the first time.
func ensureDeployKey() (string, error) {
	if b, err := os.ReadFile(ghDeployKeyPath()); err == nil && len(bytes.TrimSpace(b)) > 0 {
		return string(b), nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	blk, err := ssh.MarshalPrivateKey(priv, "fast-infra-panel")
	if err != nil {
		return "", err
	}
	privPEM := string(pem.EncodeToMemory(blk))
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	if err := installAuthorizedKey(string(ssh.MarshalAuthorizedKey(sp))); err != nil {
		return "", fmt.Errorf("install public key: %w", err)
	}
	if err := os.WriteFile(ghDeployKeyPath(), []byte(privPEM), 0o600); err != nil {
		return "", err
	}
	return privPEM, nil
}

func installAuthorizedKey(line string) error {
	if existing, _ := os.ReadFile(authKeysPath); strings.Contains(string(existing), strings.TrimSpace(line)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(authKeysPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(authKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// --- repo secrets (libsodium sealed box) ------------------------------------

func setRepoSecret(owner, repo, name, value string) error {
	var pk struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := ghGet(fmt.Sprintf("/repos/%s/%s/actions/secrets/public-key", owner, repo), &pk); err != nil {
		return err
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pk.Key)
	if err != nil || len(pubBytes) != 32 {
		return fmt.Errorf("bad repo public key")
	}
	var pub [32]byte
	copy(pub[:], pubBytes)
	sealed, err := box.SealAnonymous(nil, []byte(value), &pub, rand.Reader)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{
		"encrypted_value": base64.StdEncoding.EncodeToString(sealed),
		"key_id":          pk.KeyID,
	})
	return ghPut(fmt.Sprintf("/repos/%s/%s/actions/secrets/%s", owner, repo, name), body)
}

// --- commit the caller workflow ---------------------------------------------

// putRepoFile commits content at path (creating or updating it). It reports
// whether it actually wrote a new commit: identical content is a no-op, and the
// caller relies on that to decide whether a build will start from the push.
func putRepoFile(owner, repo, path, content, message, branch string) (bool, error) {
	var existing struct {
		SHA     string `json:"sha"`
		Content string `json:"content"`
	}
	ghGet(fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, branch), &existing)
	if existing.SHA != "" {
		// The Contents API returns base64 with embedded newlines; strip them
		// before comparing. Identical content means no commit — so no push, and
		// no build would be triggered.
		cur, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(existing.Content, "\n", ""))
		if string(cur) == content {
			return false, nil
		}
	}
	m := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	if existing.SHA != "" {
		m["sha"] = existing.SHA
	}
	b, _ := json.Marshal(m)
	if err := ghPut(fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), b); err != nil {
		return false, err
	}
	return true, nil
}

// callerWorkflow references the operator's own fast-infra fork
// (<owner>/fast-infra) so it works for any install, not just the maintainer's.
// autoDeploy toggles the push trigger (off = deploy only via workflow_dispatch),
// and paths optionally restricts the push trigger to matching files (monorepos).
func callerWorkflow(owner, app, branch string, autoDeploy bool, paths []string) string {
	var on strings.Builder
	on.WriteString("on:\n")
	if autoDeploy {
		on.WriteString("  push:\n")
		fmt.Fprintf(&on, "    branches: [%s]\n", branch)
		if len(paths) > 0 {
			on.WriteString("    paths:\n")
			for _, p := range paths {
				fmt.Fprintf(&on, "      - %q\n", p)
			}
		}
	}
	on.WriteString("  workflow_dispatch:\n")
	on.WriteString("    inputs:\n")
	on.WriteString("      tag:\n")
	on.WriteString("        description: \"Commit SHA to deploy (empty = this commit)\"\n")
	on.WriteString("        required: false\n")
	// The reusable workflow pushes to GHCR, so the caller must grant
	// packages:write. A called workflow's token can't exceed the caller's, and
	// repos default to read-only — without this the run fails at startup.
	return fmt.Sprintf(`name: deploy-%[1]s
%[2]spermissions:
  contents: read
  packages: write
jobs:
  deploy:
    uses: %[3]s/fast-infra/.github/workflows/deploy-template.yml@master
    with:
      app_name: %[1]s
      tag: ${{ inputs.tag || '' }}
    secrets: inherit
`, app, on.String(), owner)
}

// ghDispatchWorkflow kicks off the deploy workflow explicitly. Only needed when
// committing the caller workflow didn't produce a push (a re-run with identical
// content) — a fresh or changed commit already triggers the build on its own.
func ghDispatchWorkflow(owner, repo, workflowFile, branch string) error {
	body := []byte(fmt.Sprintf(`{"ref":%q}`, branch))
	req, err := http.NewRequest("POST", fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/workflows/%s/dispatches", owner, repo, workflowFile), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+ghToken())
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := ghClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// ghDispatchWorkflowRetry dispatches, retrying the 404 that GitHub returns for a
// few seconds after a workflow file is first committed but not yet registered.
func ghDispatchWorkflowRetry(owner, repo, workflowFile, branch string) error {
	var err error
	for range 6 {
		if err = ghDispatchWorkflow(owner, repo, workflowFile, branch); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return err
}

// ghAllowReusable lets the operator's other repos use the fast-infra fork's
// reusable workflow. The access policy API only applies to private/internal
// repos, so a 422 means the fork is public (already accessible) — treat that
// as success.
func ghAllowReusable(owner string) error {
	err := ghPut(fmt.Sprintf("/repos/%s/fast-infra/actions/permissions/access", owner),
		[]byte(`{"access_level":"user"}`))
	if err != nil && strings.Contains(err.Error(), "422") {
		return nil
	}
	return err
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// handleGithubDeploy scaffolds a repo for hands-off deploys: create the app,
// provision an SSH deploy key + repo secrets, and commit the caller workflow.
func handleGithubDeploy(w http.ResponseWriter, r *http.Request) {
	if ghToken() == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not connected to GitHub"})
		return
	}
	var body struct {
		Owner          string   `json:"owner"`
		Repo           string   `json:"repo"`
		Name           string   `json:"name"`
		Domain         string   `json:"domain"`
		Health         string   `json:"health"`
		Branch         string   `json:"branch"`
		Paths          []string `json:"paths"`
		Port           int      `json:"port"`
		AutoDeploy     *bool    `json:"autoDeploy"`
		ProvisionDB    bool     `json:"provisionDB"`
		ProvisionRedis bool     `json:"provisionRedis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err)
		return
	}
	if body.Branch == "" {
		body.Branch = "main"
	}
	// Auto-deploy (deploy on every push) defaults to on when unspecified.
	auto := body.AutoDeploy == nil || *body.AutoDeploy
	// App name defaults to the repo, but can differ — so the same repo can run
	// as several apps, each with its own per-app workflow file.
	name := body.Name
	if name == "" {
		name = body.Repo
	}
	// Create the app unless it already exists (re-running just refreshes the
	// secrets/workflow).
	if _, err := os.Stat(filepath.Join("apps", name)); err != nil {
		if _, err := createApp(&App{Name: name, Image: "ghcr.io/" + body.Owner + "/" + body.Repo, Domain: body.Domain, Port: body.Port, Health: body.Health}, body.ProvisionDB, body.ProvisionRedis); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "create app: " + err.Error()})
			return
		}
	}

	warnings := []string{}
	key, err := ensureDeployKey()
	if err != nil {
		warnings = append(warnings, "deploy key: "+err.Error())
	}
	secrets := []struct{ n, v string }{
		{"VPS_HOST", envOr("PANEL_VPS_HOST", os.Getenv("BASE_DOMAIN"))},
		{"VPS_USER", envOr("PANEL_VPS_USER", "root")},
		{"VPS_SSH_KEY", key},
	}
	for _, s := range secrets {
		if s.v == "" {
			warnings = append(warnings, "secret "+s.n+": empty (set PANEL_VPS_HOST?)")
			continue
		}
		if err := setRepoSecret(body.Owner, body.Repo, s.n, s.v); err != nil {
			warnings = append(warnings, "secret "+s.n+": "+err.Error())
		}
	}
	if err := ghAllowReusable(body.Owner); err != nil {
		warnings = append(warnings, "couldn't auto-allow the fast-infra reusable workflow (make "+body.Owner+"/fast-infra public or set Actions access to \"user\"): "+err.Error())
	}
	wfFile := "deploy-" + name + ".yml"
	dispatched := false
	committed, err := putRepoFile(body.Owner, body.Repo, ".github/workflows/"+wfFile, callerWorkflow(body.Owner, name, body.Branch, auto, body.Paths), "Add fast-infra deploy workflow for "+name, body.Branch)
	// Committing the workflow is itself a push, which triggers the build — but
	// only when auto-deploy is on and there's no path filter (the committed file
	// lives under .github/, so a path filter wouldn't match it). In every other
	// case (path filter set, auto-deploy off, or an unchanged re-run) that push
	// won't build, so we dispatch explicitly instead. Relying on the push when it
	// applies avoids a second concurrent build of the same app.
	pushBuilds := auto && len(body.Paths) == 0
	switch {
	case err != nil:
		warnings = append(warnings, "workflow: "+err.Error())
	case committed && pushBuilds:
		dispatched = true
	default:
		if err := ghDispatchWorkflowRetry(body.Owner, body.Repo, wfFile, body.Branch); err != nil {
			warnings = append(warnings, "couldn't start a build (run the deploy workflow from the repo's Actions tab): "+err.Error())
		} else {
			dispatched = true
		}
	}
	writeJSON(w, 200, map[string]any{"warnings": warnings, "branch": body.Branch, "name": name, "dispatched": dispatched})
}
