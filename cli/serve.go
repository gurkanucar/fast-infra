package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// panelSecret signs session cookies; regenerated each start (restart = re-login).
var panelSecret = randSecret()

func randSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// cmdServe starts the web panel. Requires PANEL_PASSWORD and the repo root.
func cmdServe(args []string) error {
	addr := ":8080"
	if len(args) == 1 {
		addr = args[0]
	}
	pw := os.Getenv("PANEL_PASSWORD")
	if pw == "" {
		return fmt.Errorf("PANEL_PASSWORD is not set — refusing to start without a login password")
	}
	if _, err := os.Stat("apps"); err != nil {
		return fmt.Errorf("apps/ not found — run from the fast-infra repo root")
	}

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("POST /api/login", handleLogin(pw))
	mux.HandleFunc("POST /api/logout", handleLogout)
	mux.HandleFunc("GET /api/me", requireAuth(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]bool{"ok": true}) }))
	mux.HandleFunc("GET /api/apps", requireAuth(handleList))
	mux.HandleFunc("POST /api/apps", requireAuth(handleCreate))
	mux.HandleFunc("GET /api/apps/{name}", requireAuth(handleDetail))
	mux.HandleFunc("DELETE /api/apps/{name}", requireAuth(handleRemove))
	mux.HandleFunc("POST /api/apps/{name}/deploy", requireAuth(handleDeploy))
	mux.HandleFunc("POST /api/apps/{name}/rollback", requireAuth(handleRollback))
	mux.HandleFunc("POST /api/apps/{name}/scale", requireAuth(handleScale))
	mux.HandleFunc("GET /api/apps/{name}/env", requireAuth(handleEnvGet))
	mux.HandleFunc("PUT /api/apps/{name}/env", requireAuth(handleEnvPut))
	mux.HandleFunc("POST /api/apps/{name}/provision", requireAuth(handleProvision))
	mux.HandleFunc("GET /api/services", requireAuth(handleServices))

	fmt.Println("panel listening on", addr)
	return http.ListenAndServe(addr, mux)
}

// --- auth -------------------------------------------------------------------

func signToken(expUnix int64) string {
	msg := strconv.FormatInt(expUnix, 10)
	mac := hmac.New(sha256.New, panelSecret)
	mac.Write([]byte(msg))
	return msg + "." + hex.EncodeToString(mac.Sum(nil))
}

func validToken(tok string) bool {
	msg, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(msg, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	mac := hmac.New(sha256.New, panelSecret)
	mac.Write([]byte(msg))
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(want)) == 1
}

func requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil || !validToken(c.Value) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		h(w, r)
	}
}

func handleLogin(password string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if subtle.ConstantTimeCompare([]byte(body.Password), []byte(password)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid password"})
			return
		}
		exp := time.Now().Add(12 * time.Hour).Unix()
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		http.SetCookie(w, &http.Cookie{
			Name: "session", Value: signToken(exp), Path: "/",
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
			Expires: time.Unix(exp, 0),
		})
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// --- app data ---------------------------------------------------------------

type appInfo struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Domain   string `json:"domain"`
	Tag      string `json:"tag"`
	Replicas int    `json:"replicas"`
	Running  int    `json:"running"`
	Healthy  int    `json:"healthy"`
	State    string `json:"state"`
}

func appInfoFor(name string) (*appInfo, error) {
	dir, err := appDir(name)
	if err != nil {
		return nil, err
	}
	app, err := loadApp(dir)
	if err != nil {
		return nil, err
	}
	tag := currentTag(dir)
	running, healthy := replicaState(dir, tag)
	state := "down"
	if running > 0 {
		if state = "up"; healthy < running {
			state = "degraded"
		}
	}
	return &appInfo{app.Name, app.Image, app.Domain, tag, app.Replicas, running, healthy, state}, nil
}

func handleList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir("apps")
	if err != nil {
		writeErr(w, err)
		return
	}
	apps := []appInfo{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := appInfoFor(e.Name()); err == nil {
			apps = append(apps, *info)
		}
	}
	writeJSON(w, 200, apps)
}

func handleDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, err := appInfoFor(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	dir := filepath.Join("apps", name)
	hist, _ := readHistory(dir)
	writeJSON(w, 200, map[string]any{"app": info, "history": hist, "db": pgHasDB(name), "redis": redisHasUser(name)})
}

// pgHasDB reports whether a Postgres database named after the app exists.
func pgHasDB(name string) bool {
	out, err := dockerOut("exec", pgContainer, "psql", "-U", "postgres", "-tAc",
		"SELECT 1 FROM pg_database WHERE datname='"+name+"'")
	return err == nil && strings.TrimSpace(out) == "1"
}

// redisHasUser reports whether a scoped Redis ACL user for the app exists.
func redisHasUser(name string) bool {
	out, err := dockerOut("exec", redisContainer, "redis-cli", "ACL", "GETUSER", name)
	return err == nil && strings.TrimSpace(out) != ""
}

func handleProvision(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := appDir(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": name + " does not exist"})
		return
	}
	var body struct {
		DB    bool `json:"db"`
		Redis bool `json:"redis"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	warnings := []string{}
	if body.DB {
		if pw, err := provisionPostgres(name); err != nil {
			warnings = append(warnings, "Postgres: "+err.Error())
		} else if err := envSet(dir, name, []string{"DATABASE_URL=postgres://" + name + ":" + pw + "@postgres:5432/" + name}); err != nil {
			warnings = append(warnings, "env update: "+err.Error())
		}
	}
	if body.Redis {
		if pw, err := provisionRedis(name); err != nil {
			warnings = append(warnings, "Redis: "+err.Error())
		} else if err := envSet(dir, name, []string{"REDIS_URL=redis://" + name + ":" + pw + "@redis:6379"}); err != nil {
			warnings = append(warnings, "env update: "+err.Error())
		}
	}
	writeJSON(w, 200, map[string]any{"warnings": warnings})
}

type serviceLink struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Desc    string `json:"desc"`
	Running bool   `json:"running"`
}

func handleServices(w http.ResponseWriter, r *http.Request) {
	base := os.Getenv("BASE_DOMAIN")
	list := []serviceLink{
		{"Adminer", "https://db." + base, "Postgres database UI", containerUp("fast-infra-adminer-1")},
		{"OpenObserve", "https://logs." + base, "Logs, traces & metrics", containerUp("fast-infra-openobserve-1")},
		{"Dozzle", "https://tail." + base, "Live container logs", containerUp("fast-infra-dozzle-1")},
		{"RabbitMQ", "https://mq." + base, "Message broker (optional)", containerUp("fast-infra-rabbitmq-1")},
	}
	writeJSON(w, 200, map[string]any{"baseDomain": base, "services": list})
}

// containerUp reports whether a container of the given name is running.
func containerUp(name string) bool {
	out, err := dockerOut("ps", "--filter", "name="+name, "--filter", "status=running", "--format", "{{.Names}}")
	return err == nil && strings.TrimSpace(out) != ""
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name           string `json:"name"`
		Image          string `json:"image"`
		Domain         string `json:"domain"`
		Port           int    `json:"port"`
		Health         string `json:"health"`
		ProvisionDB    bool   `json:"provisionDB"`
		ProvisionRedis bool   `json:"provisionRedis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err)
		return
	}
	app := &App{Name: body.Name, Image: body.Image, Domain: body.Domain, Port: body.Port, Health: body.Health}
	outcome, err := createApp(app, body.ProvisionDB, body.ProvisionRedis)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, outcome)
}

func handleDeploy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Tag string `json:"tag"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	tag := body.Tag
	if tag == "" {
		tag = "latest"
	}
	if err := deploy(name, tag); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"deployed": tag})
}

func handleRollback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := appDir(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	prev, ok := previousSuccess(dir, currentTag(dir))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no previous successful deployment"})
		return
	}
	if err := deploy(name, prev); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"rolledBackTo": prev})
}

func handleScale(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		Replicas int `json:"replicas"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Replicas < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "replicas must be >= 1"})
		return
	}
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
	app.Replicas = body.Replicas
	if err := app.save(dir); err != nil {
		writeErr(w, err)
		return
	}
	tag := currentTag(dir)
	if err := dc(dir, tag, "up", "-d", "--no-recreate", "--scale", "app="+strconv.Itoa(body.Replicas)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"replicas": body.Replicas})
}

func handleEnvGet(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join("apps", r.PathValue("name"))
	lines, err := readEnvLines(dir)
	if err != nil {
		writeErr(w, err)
		return
	}
	type kv struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	out := []kv{}
	for _, line := range lines {
		if k, ok := envKey(line); ok {
			_, v, _ := strings.Cut(strings.TrimSpace(line), "=")
			out = append(out, kv{k, v})
		}
	}
	writeJSON(w, 200, out)
}

func handleEnvPut(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir := filepath.Join("apps", name)
	var body struct {
		Set   map[string]string `json:"set"`
		Unset []string          `json:"unset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, err)
		return
	}
	if len(body.Set) > 0 {
		pairs := make([]string, 0, len(body.Set))
		for k, v := range body.Set {
			pairs = append(pairs, k+"="+v)
		}
		if err := envSet(dir, name, pairs); err != nil {
			writeErr(w, err)
			return
		}
	}
	if len(body.Unset) > 0 {
		if err := envUnset(dir, name, body.Unset); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dir, err := appDir(name)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": name + " does not exist"})
		return
	}
	keepFiles := r.URL.Query().Get("keepFiles") == "true"
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		dc(dir, currentTag(dir), "down") // best-effort
	}
	if keepFiles {
		writeJSON(w, 200, map[string]bool{"stopped": true})
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"removed": true})
}

// --- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
