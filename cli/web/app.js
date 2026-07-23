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
  $("#login").classList.toggle("hidden", view !== "login");
  $("#app").classList.toggle("hidden", view !== "app");
}

function openModal(sel) { $(sel).classList.remove("hidden"); }
function closeModals() { $$(".modal").forEach((m) => m.classList.add("hidden")); }

async function init() {
  const r = await api("GET", "/api/me");
  if (r.ok) { show("app"); loadApps(); } else { show("login"); }
}

$("#login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("#login-error").textContent = "";
  const r = await api("POST", "/api/login", { password: $("#password").value });
  if (r.ok) { $("#password").value = ""; show("app"); loadApps(); }
  else { $("#login-error").textContent = (r.data && r.data.error) || "Login failed"; }
});
$("#logout-btn").onclick = async () => { await api("POST", "/api/logout"); show("login"); };
$("#refresh-btn").onclick = loadApps;
$("#new-btn").onclick = () => openModal("#new-modal");
$$("[data-close]").forEach((b) => (b.onclick = closeModals));
$$(".modal").forEach((m) => m.addEventListener("click", (e) => { if (e.target === m) closeModals(); }));

async function loadApps() {
  const r = await api("GET", "/api/apps");
  if (!r.ok) { toast("Failed to load apps", true); return; }
  const grid = $("#apps");
  grid.innerHTML = "";
  const apps = r.data || [];
  $("#empty").classList.toggle("hidden", apps.length > 0);
  apps.forEach((a) => grid.appendChild(card(a)));
}

function esc(s) { return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

function card(a) {
  const el = document.createElement("div");
  el.className = "app-card";
  el.innerHTML = `<div class="badge ${a.state}">${a.state}</div>
    <h3>${esc(a.name)}</h3><div class="dom">${esc(a.domain)}</div>
    <div class="meta"><span>tag: ${esc(a.tag)}</span><span>${a.healthy}/${a.replicas} healthy</span></div>`;
  el.onclick = () => openDetail(a.name);
  return el;
}

async function openDetail(name) {
  const r = await api("GET", "/api/apps/" + encodeURIComponent(name));
  if (!r.ok) { toast("Not found", true); return; }
  const { app, history } = r.data;
  const hist = (history || []).slice().reverse().slice(0, 8)
    .map((h) => `<li><span class="${h.status === "success" ? "ok" : "fail"}">${h.status === "success" ? "✓" : "✗"}</span><b>${esc(h.tag)}</b><span class="muted">${new Date(h.time).toLocaleString()}</span></li>`)
    .join("") || '<li class="muted">no deployments yet</li>';
  const body = $("#detail-body");
  body.className = "detail";
  body.innerHTML = `
    <h2>${esc(app.name)} <span class="badge ${app.state}">${app.state}</span></h2>
    <div class="sub">${esc(app.image)}:${esc(app.tag)} &middot; <a href="https://${esc(app.domain)}" target="_blank" rel="noopener">${esc(app.domain)}</a></div>
    <div class="stat-row">
      <div class="stat"><b>${app.healthy}/${app.replicas}</b><span>healthy</span></div>
      <div class="stat"><b>${app.running}</b><span>running</span></div>
      <div class="stat"><b>${esc(app.tag)}</b><span>current tag</span></div>
    </div>
    <div class="section"><h4>Deploy</h4>
      <div class="inline"><input id="deploy-tag" placeholder="tag (default latest)"><button class="primary" id="do-deploy">Deploy</button><button id="do-rollback">Rollback</button></div>
    </div>
    <div class="section"><h4>Scale</h4>
      <div class="inline"><input id="scale-n" type="number" min="1" value="${app.replicas}" style="max-width:110px"><button id="do-scale">Scale</button></div>
    </div>
    <div class="section"><h4>Environment</h4><div id="env-list"></div>
      <button id="env-add" class="ghost">+ Add variable</button>
      <div class="inline" style="margin-top:.6rem"><button class="primary" id="env-save">Save env</button><span class="muted" style="font-size:.82rem">applied on next deploy</span></div>
    </div>
    <div class="section"><h4>History</h4><ul class="hist">${hist}</ul></div>
    <div class="section"><h4>Danger zone</h4><button class="danger" id="do-remove">Remove app</button></div>`;
  $("#do-deploy").onclick = () => act(name, "deploy", { tag: $("#deploy-tag").value.trim() }, "#do-deploy", "Deploying…");
  $("#do-rollback").onclick = () => act(name, "rollback", {}, "#do-rollback", "Rolling back…");
  $("#do-scale").onclick = () => act(name, "scale", { replicas: +$("#scale-n").value }, "#do-scale", "Scaling…");
  $("#do-remove").onclick = () => removeApp(name);
  $("#env-add").onclick = () => addEnvRow("", "");
  $("#env-save").onclick = () => saveEnv(name);
  loadEnv(name);
  openModal("#detail-modal");
}

async function act(name, action, body, btnSel, busy) {
  const btn = $(btnSel), orig = btn.textContent;
  btn.disabled = true; btn.textContent = busy;
  const r = await api("POST", `/api/apps/${encodeURIComponent(name)}/${action}`, body);
  btn.disabled = false; btn.textContent = orig;
  if (r.ok) { toast(`${action} succeeded`); openDetail(name); loadApps(); }
  else { toast((r.data && r.data.error) || `${action} failed`, true); }
}

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

async function removeApp(name) {
  if (!confirm(`Remove ${name}? Containers stop and apps/${name} is deleted.\nThe Postgres database and pushed images are left intact.`)) return;
  const r = await api("DELETE", `/api/apps/${encodeURIComponent(name)}`);
  if (r.ok) { toast(`${name} removed`); closeModals(); loadApps(); }
  else { toast("Remove failed", true); }
}

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

init();
