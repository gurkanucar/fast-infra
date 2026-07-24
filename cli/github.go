package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Public OAuth App client id for fast-infra's "Connect GitHub" (device flow).
// Device flow needs no client secret and no callback URL, so one id is shared
// by every install — each operator authorizes it against their own account.
const ghClientID = "Ov23liBosefcIioXQ1qi"
const ghScope = "repo workflow read:packages"

var ghClient = &http.Client{Timeout: 20 * time.Second}

// ghDeviceCode holds the pending device authorization (single operator).
var ghDeviceCode string

func ghTokenPath() string { return filepath.Join("infra", ".gh_token") }

// ghToken returns the connected token (device flow) or the env fallback.
func ghToken() string {
	if b, err := os.ReadFile(ghTokenPath()); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t
		}
	}
	return os.Getenv("PANEL_GITHUB_TOKEN")
}

func ghGet(path string, v any) error {
	req, err := http.NewRequest("GET", "https://api.github.com"+path, nil)
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
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github %s: %s", path, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func ghPostForm(u string, form url.Values, v any) error {
	req, err := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := ghClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(v)
}

// handleGithubStatus reports whether a GitHub account is connected.
func handleGithubStatus(w http.ResponseWriter, r *http.Request) {
	if ghToken() == "" {
		writeJSON(w, 200, map[string]any{"connected": false})
		return
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := ghGet("/user", &u); err != nil {
		writeJSON(w, 200, map[string]any{"connected": false})
		return
	}
	writeJSON(w, 200, map[string]any{"connected": true, "user": u.Login})
}

// handleGithubConnect starts the device flow and returns the user code.
func handleGithubConnect(w http.ResponseWriter, r *http.Request) {
	var res struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	form := url.Values{"client_id": {ghClientID}, "scope": {ghScope}}
	if err := ghPostForm("https://github.com/login/device/code", form, &res); err != nil {
		writeErr(w, err)
		return
	}
	if res.DeviceCode == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "GitHub did not return a device code"})
		return
	}
	ghDeviceCode = res.DeviceCode
	writeJSON(w, 200, map[string]any{"userCode": res.UserCode, "verificationUri": res.VerificationURI, "interval": res.Interval})
}

// handleGithubPoll exchanges the device code for a token once the user authorizes.
func handleGithubPoll(w http.ResponseWriter, r *http.Request) {
	if ghDeviceCode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no pending authorization"})
		return
	}
	var res struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	form := url.Values{
		"client_id":   {ghClientID},
		"device_code": {ghDeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if err := ghPostForm("https://github.com/login/oauth/access_token", form, &res); err != nil {
		writeErr(w, err)
		return
	}
	if res.AccessToken != "" {
		if err := os.WriteFile(ghTokenPath(), []byte(res.AccessToken+"\n"), 0o600); err != nil {
			writeErr(w, err)
			return
		}
		ghDeviceCode = ""
		writeJSON(w, 200, map[string]string{"status": "connected"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "pending", "detail": res.Error})
}

func handleGithubDisconnect(w http.ResponseWriter, r *http.Request) {
	os.Remove(ghTokenPath())
	ghDeviceCode = ""
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ghLatestImageTag returns the newest tag pushed to the app's GHCR package, so
// "Deploy" can grab the latest build even when the repo only pushes SHA tags
// (no :latest). Prefers a concrete tag over "latest" for clean history.
func ghLatestImageTag(image string) (string, bool) {
	if ghToken() == "" || !strings.HasPrefix(image, "ghcr.io/") {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(image, "ghcr.io/"), "/", 2)
	if len(parts) != 2 {
		return "", false
	}
	var versions []struct {
		Metadata struct {
			Container struct {
				Tags []string `json:"tags"`
			} `json:"container"`
		} `json:"metadata"`
	}
	if err := ghGet(fmt.Sprintf("/users/%s/packages/container/%s/versions?per_page=30", parts[0], url.PathEscape(parts[1])), &versions); err != nil {
		return "", false
	}
	for _, v := range versions { // newest first
		tags := v.Metadata.Container.Tags
		for _, t := range tags {
			if t != "latest" {
				return t, true
			}
		}
		if len(tags) > 0 {
			return tags[0], true
		}
	}
	return "", false
}

// commitCache memoises commit messages (they're immutable) so reopening an
// app's history doesn't refetch them from GitHub every time.
var (
	commitCache   = map[string]string{}
	commitCacheMu sync.Mutex
)

// ghCommitMessage returns a commit's full message, cached per owner/repo/sha.
func ghCommitMessage(owner, repo, sha string) string {
	key := owner + "/" + repo + "@" + sha
	commitCacheMu.Lock()
	if m, ok := commitCache[key]; ok {
		commitCacheMu.Unlock()
		return m
	}
	commitCacheMu.Unlock()
	var c struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := ghGet(fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha), &c); err != nil {
		return ""
	}
	commitCacheMu.Lock()
	commitCache[key] = c.Commit.Message
	commitCacheMu.Unlock()
	return c.Commit.Message
}

func looksLikeSHA(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// handleAppCommits maps the SHA tags in an app's deploy history to their commit
// messages, so the panel can show what each deployed version actually was.
func handleAppCommits(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := appDir(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	app, err := loadApp(dir)
	if err != nil {
		writeErr(w, err)
		return
	}
	owner, repo, ok := imageOwnerRepo(app.Image)
	if !ok || ghToken() == "" {
		writeJSON(w, 200, map[string]string{})
		return
	}
	hist, _ := readHistory(dir)
	seen := map[string]bool{}
	shas := []string{}
	for _, d := range hist {
		if d.Tag == "latest" || seen[d.Tag] || !looksLikeSHA(d.Tag) {
			continue
		}
		seen[d.Tag] = true
		shas = append(shas, d.Tag)
	}
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for _, sha := range shas {
		wg.Add(1)
		sem <- struct{}{}
		go func(sha string) {
			defer wg.Done()
			defer func() { <-sem }()
			if m := ghCommitMessage(owner, repo, sha); m != "" {
				mu.Lock()
				out[sha] = m
				mu.Unlock()
			}
		}(sha)
	}
	wg.Wait()
	writeJSON(w, 200, out)
}

// handleGithubBranches lists a repo's branches so the panel can offer them as
// type-ahead suggestions (still free-text, so a SHA or any ref also works).
func handleGithubBranches(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" || ghToken() == "" {
		writeJSON(w, 200, []string{})
		return
	}
	var branches []struct {
		Name string `json:"name"`
	}
	if err := ghGet(fmt.Sprintf("/repos/%s/%s/branches?per_page=100", url.PathEscape(owner), url.PathEscape(repo)), &branches); err != nil {
		writeJSON(w, 200, []string{})
		return
	}
	out := make([]string, 0, len(branches))
	for _, b := range branches {
		out = append(out, b.Name)
	}
	writeJSON(w, 200, out)
}

type ghRepo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Language      string `json:"language"`
	DefaultBranch string `json:"default_branch"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// handleGithubRepos lists the connected account's repositories, newest first.
func handleGithubRepos(w http.ResponseWriter, r *http.Request) {
	if ghToken() == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not connected to GitHub"})
		return
	}
	var repos []ghRepo
	if err := ghGet("/user/repos?per_page=100&sort=updated&affiliation=owner", &repos); err != nil {
		writeErr(w, err)
		return
	}
	type item struct {
		Name          string `json:"name"`
		FullName      string `json:"fullName"`
		Owner         string `json:"owner"`
		Language      string `json:"language"`
		DefaultBranch string `json:"defaultBranch"`
		Private       bool   `json:"private"`
	}
	out := []item{}
	for _, rp := range repos {
		if rp.Archived {
			continue
		}
		out = append(out, item{rp.Name, rp.FullName, rp.Owner.Login, rp.Language, rp.DefaultBranch, rp.Private})
	}
	writeJSON(w, 200, out)
}
