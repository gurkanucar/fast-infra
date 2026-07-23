const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

async function api(method, path, body) {
  const opts = { method, headers: {}, credentials: "same-origin" };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  let data = null;
  try { data = await res.json(); } catch { /* no body */ }
  return { ok: res.ok, status: res.status, data };
}

function toast(msg, err = false) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.toggle("err", err);
  t.classList.remove("hidden");
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.add("hidden"), 4000);
}

function show(view) {
  ["login", "app", "detail"].forEach((v) => $("#" + v).classList.toggle("hidden", v !== view));
}
function openModal(sel) { $(sel).classList.remove("hidden"); }
function closeModals() { $$(".modal").forEach((m) => m.classList.add("hidden")); }
function esc(s) { return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

async function init() {
  const r = await api("GET", "/api/me");
  if (r.ok) { toDashboard(); } else { show("login"); }
}
function toDashboard() { show("app"); loadServices(); loadApps(); }

/* ---- auth ---- */
$("#login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("#login-error").textContent = "";
  const r = await api("POST", "/api/login", { password: $("#password").value });
  if (r.ok) { $("#password").value = ""; toDashboard(); }
  else { $("#login-error").textContent = (r.data && r.data.error) || "Login failed"; }
});
const logout = async () => { await api("POST", "/api/logout"); show("login"); };
$("#logout-btn").onclick = logout;
$("#detail-logout").onclick = logout;
$("#refresh-btn").onclick = () => { loadServices(); loadApps(); };
$("#new-btn").onclick = () => openModal("#new-modal");
$("#back-btn").onclick = toDashboard;
$$("[data-close]").forEach((b) => (b.onclick = closeModals));
$$(".modal").forEach((m) => m.addEventListener("click", (e) => { if (e.target === m) closeModals(); }));
$$(".theme-toggle").forEach((b) => (b.onclick = toggleTheme));

/* ---- services ---- */
async function loadServices() {
  const r = await api("GET", "/api/services");
  const box = $("#services");
  box.innerHTML = "";
  const svcs = ((r.data && r.data.services) || []).filter((s) => s.running);
  if (!svcs.length) { box.innerHTML = '<p class="muted">No service UIs detected.</p>'; return; }
  svcs.forEach((s) => box.appendChild(svcCard(s)));
}
function credRow(label, val, mask) {
  return `<div class="cred"><span class="ck">${label}</span><code>${mask ? "••••••••" : esc(val)}</code><button class="copy" data-val="${esc(val)}" title="Copy ${label}">copy</button></div>`;
}
function svcCard(s) {
  const el = document.createElement("div");
  el.className = "svc-card";
  const creds = (s.user || s.pass)
    ? `<div class="creds">${s.user ? credRow("user", s.user) : ""}${s.pass ? credRow("pass", s.pass, true) : ""}</div>`
    : "";
  el.innerHTML = `<a class="svc-open" href="${esc(s.url)}" target="_blank" rel="noopener" title="${esc(s.desc)}">${esc(s.name)} <span class="ext">↗</span></a>${creds}`;
  el.querySelectorAll(".copy").forEach((b) => (b.onclick = () => copyText(b.dataset.val)));
  return el;
}
async function copyText(text) {
  try { await navigator.clipboard.writeText(text); toast("Copied to clipboard"); }
  catch { toast("Copy failed — copy it manually", true); }
}

/* ---- theme ---- */
function initTheme() { document.documentElement.setAttribute("data-theme", localStorage.getItem("theme") || "dark"); }
function toggleTheme() {
  const next = document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light";
  document.documentElement.setAttribute("data-theme", next);
  localStorage.setItem("theme", next);
}

/* ---- app list ---- */
async function loadApps() {
  const r = await api("GET", "/api/apps");
  if (!r.ok) { toast("Failed to load apps", true); return; }
  const grid = $("#apps");
  grid.innerHTML = "";
  const apps = r.data || [];
  $("#empty").classList.toggle("hidden", apps.length > 0);
  apps.forEach((a) => grid.appendChild(appCard(a)));
}
function appCard(a) {
  const el = document.createElement("div");
  el.className = "app-card";
  el.innerHTML = `<span class="led ${a.state}" title="${a.state}"></span>
    <div class="ac-main"><h3>${esc(a.name)}</h3><span class="dom">${esc(a.domain)}</span></div>
    <div class="meta"><span class="chip">${esc(a.tag)}</span><span>${a.healthy}/${a.replicas}</span></div>`;
  el.onclick = () => openDetail(a.name);
  return el;
}

/* ---- detail page ---- */
async function openDetail(name) {
  const r = await api("GET", "/api/apps/" + encodeURIComponent(name));
  if (!r.ok) { toast("Not found", true); return; }
  const { app, history, db, redis } = r.data;
  $("#detail-name").textContent = app.name;
  const hist = (history || []).slice().reverse().slice(0, 12)
    .map((h) => `<li><span class="${h.status === "success" ? "ok" : "fail"}">${h.status === "success" ? "✓" : "✗"}</span><b>${esc(h.tag)}</b><span class="muted">${new Date(h.time).toLocaleString()}</span></li>`)
    .join("") || '<li class="muted">no deployments yet</li>';
  const dbBadge = db ? '<span class="badge up">provisioned</span>' : '<span class="badge down">not set up</span>';
  const rdBadge = redis ? '<span class="badge up">scoped user</span>' : '<span class="badge down">not set up</span>';

  $("#detail-body").innerHTML = `
    <div class="detail-head">
      <div>
        <div class="sub"><span class="badge ${app.state}">${app.state}</span> ${esc(app.image)}:${esc(app.tag)}</div>
        <a class="dom-link" href="https://${esc(app.domain)}" target="_blank" rel="noopener">${esc(app.domain)} ↗</a>
      </div>
      <div class="stat-row">
        <div class="stat"><b>${app.healthy}/${app.replicas}</b><span>healthy</span></div>
        <div class="stat"><b>${app.running}</b><span>running</span></div>
      </div>
    </div>
    <div class="detail-grid">
      <div class="col">
        <div class="card sect">
          <h4>Deploy</h4>
          <div class="inline"><input id="deploy-tag" placeholder="tag (default latest)"><button class="primary" id="do-deploy">Deploy</button><button id="do-rollback">Rollback</button></div>
          <div class="inline" style="margin-top:.55rem"><button id="do-start">Start</button><button id="do-stop">Stop</button><span class="muted small">start = redeploy current tag · stop = down (keeps files)</span></div>
        </div>
        <div class="card sect">
          <h4>Scale</h4>
          <div class="inline"><input id="scale-n" type="number" min="1" value="${app.replicas}" style="max-width:110px"><button id="do-scale">Scale</button></div>
        </div>
        <div class="card sect">
          <h4>Environment</h4>
          <div id="env-list"></div>
          <button id="env-add" class="ghost">+ Add variable</button>
          <div class="inline" style="margin-top:.6rem"><button class="primary" id="env-save">Save env</button><span class="muted small">applied on next deploy</span></div>
        </div>
      </div>
      <div class="col">
        <div class="card sect">
          <h4>Data</h4>
          <div class="data-row">Postgres ${dbBadge} <button class="ghost small" id="prov-db">${db ? "Reset" : "Create DB + user"}</button></div>
          <div class="data-row">Redis ${rdBadge} <button class="ghost small" id="prov-redis">${redis ? "Reset" : "Create scoped user"}</button></div>
          <p class="muted small">Credentials are written to the app's .env; apply with a deploy.</p>
        </div>
        <div class="card sect">
          <h4>History</h4>
          <ul class="hist">${hist}</ul>
        </div>
        <div class="card sect danger-card">
          <h4>Danger zone</h4>
          <label class="check small"><input type="checkbox" id="keep-files"> Keep files (only stop containers)</label>
          <button class="danger" id="do-remove">Remove app</button>
        </div>
      </div>
    </div>`;

  $("#do-deploy").onclick = () => act(name, "deploy", { tag: $("#deploy-tag").value.trim() }, "#do-deploy", "Deploying…");
  $("#do-rollback").onclick = () => act(name, "rollback", {}, "#do-rollback", "Rolling back…");
  $("#do-start").onclick = () => act(name, "start", {}, "#do-start", "Starting…");
  $("#do-stop").onclick = () => act(name, "stop", {}, "#do-stop", "Stopping…");
  $("#do-scale").onclick = () => act(name, "scale", { replicas: +$("#scale-n").value }, "#do-scale", "Scaling…");
  $("#prov-db").onclick = () => provision(name, { db: true }, "#prov-db");
  $("#prov-redis").onclick = () => provision(name, { redis: true }, "#prov-redis");
  $("#do-remove").onclick = () => removeApp(name, $("#keep-files").checked);
  $("#env-add").onclick = () => addEnvRow("", "");
  $("#env-save").onclick = () => saveEnv(name);
  loadEnv(name);
  show("detail");
  window.scrollTo(0, 0);
}

async function act(name, action, body, btnSel, busy) {
  const btn = $(btnSel), orig = btn.textContent;
  btn.disabled = true; btn.textContent = busy;
  const r = await api("POST", `/api/apps/${encodeURIComponent(name)}/${action}`, body);
  btn.disabled = false; btn.textContent = orig;
  if (r.ok) { toast(`${action} succeeded`); openDetail(name); }
  else { toast((r.data && r.data.error) || `${action} failed`, true); }
}

async function provision(name, body, btnSel) {
  const btn = $(btnSel), orig = btn.textContent;
  btn.disabled = true; btn.textContent = "Working…";
  const r = await api("POST", `/api/apps/${encodeURIComponent(name)}/provision`, body);
  btn.disabled = false; btn.textContent = orig;
  const warns = (r.data && r.data.warnings) || [];
  if (r.ok && !warns.length) { toast("Provisioned — deploy to apply"); openDetail(name); }
  else { toast(warns.join("; ") || "Provision failed", true); }
}

/* ---- env editor ---- */
let envRemoved = new Set(), envOrig = {};
async function loadEnv(name) {
  envRemoved = new Set(); envOrig = {};
  const r = await api("GET", `/api/apps/${encodeURIComponent(name)}/env`);
  $("#env-list").innerHTML = "";
  (r.data || []).forEach((kv) => { envOrig[kv.key] = kv.value; addEnvRow(kv.key, kv.value); });
}
function addEnvRow(k, v) {
  const row = document.createElement("div");
  row.className = "env-row";
  row.innerHTML = `<input class="k" placeholder="KEY" value="${esc(k)}"><input class="v" placeholder="value" value="${esc(v)}"><button class="ghost" title="remove">✕</button>`;
  row.querySelector("button").onclick = () => {
    const key = row.querySelector(".k").value.trim();
    if (key && envOrig[key] !== undefined) envRemoved.add(key);
    row.remove();
  };
  $("#env-list").appendChild(row);
}
async function saveEnv(name) {
  const set = {};
  $$("#env-list .env-row").forEach((row) => {
    const k = row.querySelector(".k").value.trim();
    if (k) { set[k] = row.querySelector(".v").value; envRemoved.delete(k); }
  });
  const r = await api("PUT", `/api/apps/${encodeURIComponent(name)}/env`, { set, unset: [...envRemoved] });
  if (r.ok) { toast("Environment saved"); loadEnv(name); }
  else { toast((r.data && r.data.error) || "Env save failed", true); }
}

async function removeApp(name, keepFiles) {
  const msg = keepFiles
    ? `Stop ${name}'s containers? Files are kept so you can redeploy later.`
    : `Remove ${name}? Containers stop and apps/${name} is deleted.\nThe Postgres database and pushed images are left intact.`;
  if (!confirm(msg)) return;
  const r = await api("DELETE", `/api/apps/${encodeURIComponent(name)}?keepFiles=${keepFiles}`);
  if (r.ok) { toast(keepFiles ? `${name} stopped` : `${name} removed`); toDashboard(); }
  else { toast("Remove failed", true); }
}

/* ---- create ---- */
$("#new-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = e.target, g = (n) => f.elements[n];
  $(".new-error", f).textContent = "";
  const btn = $('button[type=submit]', f), orig = btn.textContent;
  btn.disabled = true; btn.textContent = "Creating…";
  const body = {
    name: g("name").value.trim(), image: g("image").value.trim(), domain: g("domain").value.trim(),
    port: +g("port").value || 8080, health: g("health").value.trim() || "/health",
    provisionDB: g("provisionDB").checked, provisionRedis: g("provisionRedis").checked,
  };
  const r = await api("POST", "/api/apps", body);
  btn.disabled = false; btn.textContent = orig;
  if (r.ok) {
    const o = r.data || {};
    if (o.Warnings && o.Warnings.length) toast(o.Warnings.join("; "), true);
    else toast("App created");
    f.reset(); closeModals(); loadApps();
  } else { $(".new-error", f).textContent = (r.data && r.data.error) || "Create failed"; }
});

initTheme();
init();
