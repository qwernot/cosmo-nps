const state = {
  me: null,
  users: [],
  tunnels: [],
  engines: [],
  runtime: null,
  view: "dashboard",
};

const titles = {
  dashboard: ["概览", "查看账号、端口池和隧道使用情况。"],
  users: ["用户", "管理员创建账号，分配固定端口池。"],
  tunnels: ["隧道", "创建 FRP 或 NPS 隧道，端口只能来自分配范围。"],
  configs: ["配置", "查看自己的 frpc.toml 和 npc 启动命令。"],
  engines: ["引擎", "启动、停止和检查 FRP/NPS 运行状态。"],
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => Array.from(document.querySelectorAll(selector));

function isAdmin() {
  return state.me && state.me.role === "admin";
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  if (res.status === 401) {
    showLogin();
    throw new Error("请先登录");
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  const type = res.headers.get("content-type") || "";
  if (type.includes("application/json")) return res.json();
  return res.text();
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function formatPools(pools) {
  if (!pools || pools.length === 0) return "-";
  return pools.map((p) => (p.start === p.end ? `${p.start}` : `${p.start}-${p.end}`)).join(",");
}

function formatDomains(domains) {
  if (!domains || domains.length === 0) return "-";
  return domains.join(",");
}

function isDomainMode(mode) {
  return mode === "http" || mode === "https";
}

function tunnelEntry(t) {
  return isDomainMode(t.mode) ? formatDomains(t.domains) : (t.remotePort || "-");
}

function toast(message) {
  const el = $("#toast");
  el.textContent = message;
  el.classList.add("show");
  window.clearTimeout(toast.timer);
  toast.timer = window.setTimeout(() => el.classList.remove("show"), 2200);
}

function showLogin() {
  document.body.classList.add("login-mode");
}

function showApp() {
  document.body.classList.remove("login-mode");
}

async function boot() {
  try {
    state.me = await api("/api/me");
    showApp();
    await refresh();
  } catch {
    showLogin();
  }
}

async function refresh() {
  const requests = [api("/api/runtime"), api("/api/users"), api("/api/tunnels")];
  const [runtime, users, tunnels, engines = []] = await Promise.all(requests);
  state.runtime = runtime;
  state.users = users;
  state.tunnels = tunnels;
  state.engines = engines;
  render();
}

function render() {
  $("#session-user").textContent = `${state.me.name} (${state.me.role})`;
  $("#runtime-server").textContent = `${state.runtime?.client?.serverAddr || "-"}:${state.runtime?.client?.frpServerPort || "-"}`;
  $$(".admin-only").forEach((el) => el.classList.toggle("hidden", !isAdmin()));
  if (!isAdmin() && state.view === "users") {
    switchView("dashboard");
  }
  renderUserOptions();
  renderMetrics();
  renderUsers();
  renderTunnels();
  renderDashboardTables();
}

function renderMetrics() {
  $("#metric-users").textContent = state.users.length;
  $("#metric-tunnels").textContent = state.tunnels.length;
  $("#metric-frp").textContent = state.tunnels.filter((t) => t.engine === "frp").length;
  $("#metric-nps").textContent = state.tunnels.filter((t) => t.engine === "nps").length;
}

function renderDashboardTables() {
  $("#dashboard-users").innerHTML = state.users.slice(0, 6).map((u) => `
    <tr>
      <td>${escapeHtml(u.name)}</td>
      <td title="${escapeHtml(formatDomains(u.domainPools))}">${escapeHtml(formatPools(u.portPools))}</td>
      <td>${statusBadge(u.enabled)}</td>
    </tr>
  `).join("") || emptyRow(3);

  $("#dashboard-tunnels").innerHTML = state.tunnels.slice(0, 6).map((t) => `
    <tr>
      <td title="${escapeHtml(t.id)}">${escapeHtml(t.id)}</td>
      <td>${engineBadge(t.engine)}</td>
      <td>${escapeHtml(tunnelEntry(t))}</td>
    </tr>
  `).join("") || emptyRow(3);
}

function renderUsers() {
  $("#users-table").innerHTML = state.users.map((u) => `
    <tr>
      <td>${escapeHtml(u.name)}</td>
      <td title="${escapeHtml(formatPools(u.portPools))}">${escapeHtml(formatPools(u.portPools))}</td>
      <td title="${escapeHtml(formatDomains(u.domainPools))}">${escapeHtml(formatDomains(u.domainPools))}</td>
      <td>${u.maxPorts || 0}</td>
      <td>
        <span class="badge ${u.hasFrpToken ? "ok" : "warn"}">FRP</span>
        <span class="badge ${u.hasNpsVerifyKey ? "ok" : "warn"}">NPS</span>
      </td>
      <td>${statusBadge(u.enabled)}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-edit-user="${escapeHtml(u.name)}">编辑</button>
          <button class="button small danger" data-delete-user="${escapeHtml(u.name)}">删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(7);
}

function renderTunnels() {
  $("#tunnels-table").innerHTML = state.tunnels.map((t) => `
    <tr>
      <td title="${escapeHtml(t.id)}">${escapeHtml(t.id)}</td>
      <td>${escapeHtml(t.userName)}</td>
      <td>${engineBadge(t.engine)}</td>
      <td>${escapeHtml(t.mode)}</td>
      <td>${escapeHtml(tunnelEntry(t))}</td>
      <td>${escapeHtml(`${t.localIp || "-"}:${t.localPort || "-"}`)}</td>
      <td>${statusBadge(t.enabled)}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-edit-tunnel="${escapeHtml(t.id)}">编辑</button>
          <button class="button small danger" data-delete-tunnel="${escapeHtml(t.id)}">删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(8);
}

function renderEngines() {
  if (!isAdmin()) return;
  $("#engines-table").innerHTML = state.engines.map((e) => `
    <tr>
      <td>${engineBadge(e.engine)}</td>
      <td>${e.configured ? '<span class="badge ok">已配置</span>' : '<span class="badge warn">未配置</span>'}</td>
      <td>${e.running ? `<span class="badge ok">运行 PID ${e.pid}</span>` : '<span class="badge warn">未运行</span>'}</td>
      <td>${e.port ? `${e.port} ${e.portOpen ? '<span class="badge ok">open</span>' : '<span class="badge warn">closed</span>'}` : "-"}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-start-engine="${escapeHtml(e.engine)}" ${e.configured ? "" : "disabled"}>启动</button>
          <button class="button small danger" data-stop-engine="${escapeHtml(e.engine)}">停止</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(5);
  const engineCfg = state.runtime?.engines || {};
  $("#engine-help").textContent = [
    "当前启动参数：",
    `frps: ${engineCfg.frpsBin || "(未配置)"}`,
    `frps config: ${engineCfg.frpsConfig || "(未配置)"}`,
    `nps: ${engineCfg.npsBin || "(未配置)"}`,
    `nps workdir: ${engineCfg.npsWorkDir || "(未配置)"}`,
    `FRP 用户同步文件: ${state.runtime?.frpUsersPath || "-"}`,
    `配置导出目录: ${state.runtime?.configOutDir || "-"}`,
    "",
    "示例：",
    ".\\tunnel-control.exe -addr :18088 -public-addr 你的服务器IP \\",
    "  -frps-bin C:\\path\\frps.exe -frps-config C:\\path\\frps.toml \\",
    "  -nps-bin C:\\path\\nps.exe -nps-workdir C:\\path\\nps",
  ].join("\n");
}

function renderUserOptions() {
  const options = state.users.map((u) => `<option value="${escapeHtml(u.name)}">${escapeHtml(u.name)}</option>`).join("");
  $('select[name="userName"]').innerHTML = options;
  $("#config-user").innerHTML = options;
  $('select[name="userName"]').disabled = !isAdmin();
}

function statusBadge(enabled) {
  return `<span class="badge ${enabled ? "ok" : "warn"}">${enabled ? "启用" : "停用"}</span>`;
}

function engineBadge(engine) {
  return `<span class="badge engine-${escapeHtml(engine)}">${escapeHtml(engine).toUpperCase()}</span>`;
}

function emptyRow(cols) {
  return `<tr><td colspan="${cols}">暂无数据</td></tr>`;
}

function switchView(view) {
  if (view === "engines") return;
  if (!isAdmin() && view === "users") return;
  state.view = view;
  $$(".nav-item").forEach((btn) => btn.classList.toggle("active", btn.dataset.view === view));
  $$(".view").forEach((el) => el.classList.toggle("active", el.id === `view-${view}`));
  $("#page-title").textContent = titles[view][0];
  $("#page-subtitle").textContent = titles[view][1];
}

function formData(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function field(form, name) {
  return form.elements.namedItem(name);
}

function clearUserForm() {
  $("#user-form").reset();
  $('#user-form input[name="enabled"]').checked = true;
  $('#user-form select[name="role"]').value = "user";
  $('#user-form input[name="maxPorts"]').value = "3";
}

function clearTunnelForm() {
  $("#tunnel-form").reset();
  $('#tunnel-form input[name="id"]').value = "";
  $('#tunnel-form input[name="localIp"]').value = "127.0.0.1";
  $('#tunnel-form input[name="enabled"]').checked = true;
  if (state.users[0]) $('select[name="userName"]').value = state.users[0].name;
  updateTunnelModeFields();
}

function editUser(name) {
  if (!isAdmin()) return;
  const user = state.users.find((u) => u.name === name);
  if (!user) return;
  const form = $("#user-form");
  field(form, "name").value = user.name;
  field(form, "password").value = "";
  field(form, "role").value = user.role;
  field(form, "maxPorts").value = user.maxPorts || 0;
  field(form, "portPool").value = formatPools(user.portPools);
  field(form, "domainPool").value = formatDomains(user.domainPools);
  field(form, "frpToken").value = "";
  field(form, "npsVerifyKey").value = "";
  field(form, "enabled").checked = user.enabled;
  switchView("users");
}

function editTunnel(id) {
  const tunnel = state.tunnels.find((t) => t.id === id);
  if (!tunnel) return;
  const form = $("#tunnel-form");
  field(form, "id").value = tunnel.id;
  field(form, "userName").value = tunnel.userName;
  field(form, "engine").value = tunnel.engine;
  field(form, "mode").value = tunnel.mode;
  field(form, "remotePort").value = tunnel.remotePort || "";
  field(form, "localIp").value = tunnel.localIp || "127.0.0.1";
  field(form, "localPort").value = tunnel.localPort || "";
  field(form, "domains").value = (tunnel.domains || []).join(",");
  field(form, "remark").value = tunnel.remark || "";
  field(form, "enabled").checked = tunnel.enabled;
  updateTunnelModeFields();
  switchView("tunnels");
}

function updateTunnelModeFields() {
  const form = $("#tunnel-form");
  const mode = field(form, "mode").value;
  field(form, "remotePort").disabled = isDomainMode(mode);
  field(form, "remotePort").required = !isDomainMode(mode);
  field(form, "domains").required = isDomainMode(mode);
  if (isDomainMode(mode)) {
    field(form, "remotePort").value = "";
  }
}

async function login(event) {
  event.preventDefault();
  const data = formData(event.currentTarget);
  state.me = await api("/api/login", {
    method: "POST",
    body: JSON.stringify({ name: data.name.trim(), password: data.password }),
  });
  showApp();
  await refresh();
  toast("已登录");
}

async function logout() {
  await api("/api/logout", { method: "POST" });
  state.me = null;
  showLogin();
}

async function changeOwnPassword() {
  const target = isAdmin() ? (window.prompt("用户名", state.me.name) || "").trim() : state.me.name;
  if (!target) return;
  const oldPassword = isAdmin() ? "" : window.prompt("旧密码") || "";
  if (!isAdmin() && !oldPassword) return;
  const newPassword = window.prompt("新密码") || "";
  if (!newPassword) return;
  await api("/api/password", {
    method: "POST",
    body: JSON.stringify({ name: target, oldPassword, newPassword }),
  });
  toast("密码已修改");
}

async function saveUser(event) {
  event.preventDefault();
  if (!isAdmin()) return;
  const form = event.currentTarget;
  const data = formData(form);
  const body = {
    name: data.name.trim(),
    password: data.password,
    role: data.role,
    enabled: field(form, "enabled").checked,
    portPool: data.portPool.trim(),
    domainPool: data.domainPool.trim(),
    maxPorts: Number(data.maxPorts || 0),
    frpToken: data.frpToken.trim(),
    npsVerifyKey: data.npsVerifyKey.trim(),
  };
  await api("/api/users", { method: "POST", body: JSON.stringify(body) });
  clearUserForm();
  await refresh();
  toast("用户已保存");
}

async function saveTunnel(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const data = formData(form);
  const body = {
    id: data.id,
    userName: isAdmin() ? data.userName : state.me.name,
    engine: data.engine,
    mode: data.mode,
    remotePort: isDomainMode(data.mode) ? 0 : Number(data.remotePort || 0),
    localIp: data.localIp.trim() || "127.0.0.1",
    localPort: Number(data.localPort || 0),
    domains: data.domains.split(",").map((v) => v.trim()).filter(Boolean),
    remark: data.remark.trim(),
    enabled: field(form, "enabled").checked,
  };
  if (body.id) {
    await api(`/api/tunnels/${encodeURIComponent(body.id)}`, { method: "PUT", body: JSON.stringify(body) });
  } else {
    delete body.id;
    await api("/api/tunnels", { method: "POST", body: JSON.stringify(body) });
  }
  clearTunnelForm();
  await refresh();
  toast("隧道已保存");
}

async function deleteUser(name) {
  if (!isAdmin()) return;
  if (!confirm(`删除用户 ${name} 及其隧道？`)) return;
  await api(`/api/users/${encodeURIComponent(name)}`, { method: "DELETE" });
  await refresh();
  toast("用户已删除");
}

async function deleteTunnel(id) {
  if (!confirm(`删除隧道 ${id}？`)) return;
  await api(`/api/tunnels/${encodeURIComponent(id)}`, { method: "DELETE" });
  await refresh();
  toast("隧道已删除");
}

async function loadConfig(kind) {
  const user = $("#config-user").value;
  if (!user) {
    toast("先创建用户");
    return;
  }
  const path = kind === "frp" ? "frpc.toml" : "npc-command";
  const text = await api(`/api/users/${encodeURIComponent(user)}/${path}`, { headers: {} });
  $("#config-title").textContent = kind === "frp" ? "frpc.toml" : "npc";
  $("#config-output").textContent = text;
}

async function copyConfig() {
  const text = $("#config-output").textContent;
  if (!text) return;
  await navigator.clipboard.writeText(text);
  toast("已复制");
}

async function engineAction(engine, action) {
  if (!isAdmin()) return;
  await api(`/api/engines/${encodeURIComponent(engine)}/${action}`, { method: "POST" });
  await refresh();
  toast(action === "start" ? "引擎已启动" : "引擎已停止");
}

async function syncFRPUsers() {
  if (!isAdmin()) return;
  const result = await api("/api/sync/frp-users", { method: "POST" });
  toast("运行状态已同步");
}

async function exportConfigs() {
  if (!isAdmin()) return;
  const result = await api("/api/export/configs", { method: "POST" });
  toast(`已导出到 ${result.dir}`);
  if (state.view === "engines") {
    await refresh();
  }
}

document.addEventListener("click", async (event) => {
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  if (target.dataset.view) switchView(target.dataset.view);
  if (target.dataset.jump) switchView(target.dataset.jump);
  if (target.dataset.editUser) editUser(target.dataset.editUser);
  if (target.dataset.editTunnel) editTunnel(target.dataset.editTunnel);
  if (target.dataset.deleteUser) await deleteUser(target.dataset.deleteUser).catch((err) => toast(err.message));
  if (target.dataset.deleteTunnel) await deleteTunnel(target.dataset.deleteTunnel).catch((err) => toast(err.message));
  if (target.dataset.startEngine) await engineAction(target.dataset.startEngine, "start").catch((err) => toast(err.message));
  if (target.dataset.stopEngine) await engineAction(target.dataset.stopEngine, "stop").catch((err) => toast(err.message));
});

$("#login-form").addEventListener("submit", (event) => login(event).catch((err) => toast(err.message)));
$("#logout-button").addEventListener("click", () => logout().catch((err) => toast(err.message)));
$("#password-button").addEventListener("click", () => changeOwnPassword().catch((err) => toast(err.message)));
$("#refresh-button").addEventListener("click", () => refresh().then(() => toast("已刷新")).catch((err) => toast(err.message)));
const refreshEngines = $("#refresh-engines");
if (refreshEngines) refreshEngines.addEventListener("click", () => refresh().then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#user-form").addEventListener("submit", (event) => saveUser(event).catch((err) => toast(err.message)));
$("#tunnel-form").addEventListener("submit", (event) => saveTunnel(event).catch((err) => toast(err.message)));
$('select[name="mode"]').addEventListener("change", updateTunnelModeFields);
$("#clear-user-form").addEventListener("click", clearUserForm);
$("#clear-tunnel-form").addEventListener("click", clearTunnelForm);
$("#load-frpc").addEventListener("click", () => loadConfig("frp").catch((err) => toast(err.message)));
$("#load-npc").addEventListener("click", () => loadConfig("nps").catch((err) => toast(err.message)));
$("#copy-config").addEventListener("click", () => copyConfig().catch((err) => toast(err.message)));
$("#export-configs").addEventListener("click", () => exportConfigs().catch((err) => toast(err.message)));

boot();
updateTunnelModeFields();
