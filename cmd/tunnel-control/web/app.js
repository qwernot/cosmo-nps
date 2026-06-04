const state = {
  me: null,
  users: [],
  tunnels: [],
  engines: [],
  runtime: null,
  diagnostics: null,
  clients: null,
  availability: null,
  logs: null,
  view: "dashboard",
};

const titles = {
  dashboard: ["概览", "统一用户、端口池、域名池和双引擎隧道。"],
  users: ["用户", "管理员创建账号，分配固定端口池和域名池。"],
  tunnels: ["隧道", "创建 FRP 或 NPS 隧道；TCP/UDP/SOCKS5 用端口，HTTP/HTTPS 用域名。"],
  configs: ["配置", "查看用户自己的 frpc.toml 和 npc 启动命令。"],
  engines: ["引擎", "查看单容器内置 FRP/NPS 的运行状态和对外端口。"],
  logs: ["日志", "查看后台、FRP、NPS 最近运行日志。"],
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
  return isDomainMode(t.mode) ? formatDomains(t.domains) : t.remotePort || "-";
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
  const requests = {
    runtime: api("/api/runtime"),
    users: api("/api/users"),
    tunnels: api("/api/tunnels"),
  };
  if (state.view === "dashboard" || state.view === "users") {
    requests.diagnostics = api("/api/diagnostics");
  }
  if (state.view === "dashboard" || state.view === "users" || state.view === "tunnels") {
    requests.clients = api("/api/clients");
  }
  if (state.view === "dashboard" || state.view === "tunnels") {
    requests.availability = api("/api/availability");
  }
  if (isAdmin() && state.view === "engines") {
    requests.engines = api("/api/engines");
  }
  if (isAdmin() && state.view === "logs") {
    requests.logs = loadLogs(false);
  }
  const entries = await Promise.all(Object.entries(requests).map(async ([key, request]) => [key, await request]));
  for (const [key, value] of entries) {
    if (key === "runtime") state.runtime = value;
    if (key === "users") state.users = value;
    if (key === "tunnels") state.tunnels = value;
    if (key === "diagnostics") state.diagnostics = value;
    if (key === "clients") state.clients = value;
    if (key === "availability") state.availability = value;
    if (key === "engines") state.engines = value;
    if (key === "logs") state.logs = value;
  }
  render();
}

function render() {
  $("#session-user").textContent = `${state.me.name} (${state.me.role})`;
  const client = state.runtime?.client || {};
  $("#runtime-server").textContent = `${client.serverAddr || "-"}:${client.frpServerPort || "-"}`;
  $$(".admin-only").forEach((el) => el.classList.toggle("hidden", !isAdmin()));
  if (!isAdmin() && (state.view === "users" || state.view === "engines" || state.view === "logs")) {
    switchView("dashboard");
  }
  renderUserOptions();
  if (state.view === "dashboard") {
    renderMetrics();
    renderDashboardTables();
    renderDiagnostics();
  }
  if (state.view === "users") {
    renderUsers();
    renderDiagnostics();
  }
  if (state.view === "tunnels") {
    renderTunnels();
    updateTunnelModeFields();
  }
  if (state.view === "engines") renderEngines();
  if (state.view === "logs") renderLogs();
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
      <td title="${escapeHtml(`端口: ${formatPools(u.portPools)} 域名: ${formatDomains(u.domainPools)}`)}">${escapeHtml(formatPools(u.portPools))}</td>
      <td>${userOnlineBadges(u)}</td>
      <td>${statusBadge(u.enabled)}</td>
    </tr>
  `).join("") || emptyRow(4);

  $("#dashboard-tunnels").innerHTML = state.tunnels.slice(0, 6).map((t) => `
    <tr>
      <td title="${escapeHtml(t.id)}">${escapeHtml(t.id)}</td>
      <td>${engineBadge(t.engine)}</td>
      <td>${escapeHtml(tunnelEntry(t))}</td>
      <td>${tunnelAvailabilityBadge(t)}</td>
    </tr>
  `).join("") || emptyRow(4);
}

function renderUsers() {
  $("#users-table").innerHTML = state.users.map((u) => `
    <tr>
      <td>${escapeHtml(u.name)}</td>
      <td title="${escapeHtml(formatPools(u.portPools))}">${escapeHtml(formatPools(u.portPools))}</td>
      <td title="${escapeHtml(formatDomains(u.domainPools))}">${escapeHtml(formatDomains(u.domainPools))}</td>
      <td>${u.maxPorts || 0}</td>
      <td>${resourceSummary(u.name)}</td>
      <td>
        <span class="badge ${u.hasFrpToken ? "ok" : "warn"}">FRP</span>
        <span class="badge ${u.hasNpsVerifyKey ? "ok" : "warn"}">NPS</span>
      </td>
      <td>${userOnlineBadges(u)}</td>
      <td>${statusBadge(u.enabled)}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-edit-user="${escapeHtml(u.name)}">编辑</button>
          <button class="button small danger" data-delete-user="${escapeHtml(u.name)}">删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(9);
}

function renderDiagnostics() {
  const diagnostics = state.diagnostics || {};
  const generatedAt = diagnostics.generatedAt ? new Date(diagnostics.generatedAt).toLocaleString() : "-";
  $("#diagnostics-time").textContent = `更新 ${generatedAt}`;
  const checks = diagnostics.checks || [];
  $("#diagnostic-checks").innerHTML = checks.length ? checks.map((check) => `
    <div class="check-item">
      <span>${escapeHtml(check.name)}</span>
      <strong>${check.port ? `${escapeHtml(check.host)}:${check.port}` : "-"}</strong>
      <span class="badge ${check.open ? "ok" : "warn"}">${check.open ? "正常" : "异常"}</span>
    </div>
  `).join("") : `<div class="muted-box">${isAdmin() ? "暂无监听自检数据" : "管理员可查看监听自检"}</div>`;
  const resources = diagnostics.resources || [];
  $("#diagnostic-resources").innerHTML = resources.length ? resources.map((usage) => `
    <div class="resource-card">
      <div class="resource-head">
        <strong>${escapeHtml(usage.userName)}</strong>
        <span>${usage.tunnelLimit > 0 ? `${usage.tunnelUsed}/${usage.tunnelLimit} 隧道` : `${usage.tunnelUsed} 隧道`}</span>
      </div>
      ${resourceMeter("TCP", usage.tcpUsed, usage.portTotal)}
      ${resourceMeter("UDP", usage.udpUsed, usage.portTotal)}
      ${resourceMeter("域名", usage.domainUsed, usage.hasWildcard ? -1 : usage.domainTotal)}
    </div>
  `).join("") : '<div class="muted-box">暂无资源数据</div>';
}

function resourceSummary(userName) {
  const usage = resourceFor(userName);
  if (!usage) return "-";
  const domainTotal = usage.hasWildcard ? "不限" : usage.domainTotal;
  return `
    <div class="resource-mini">
      <span>TCP ${usage.tcpUsed}/${usage.portTotal}</span>
      <span>UDP ${usage.udpUsed}/${usage.portTotal}</span>
      <span>域名 ${usage.domainUsed}/${domainTotal}</span>
    </div>
  `;
}

function resourceFor(userName) {
  return (state.diagnostics?.resources || []).find((item) => item.userName === userName);
}

function clients() {
  return state.clients?.clients || [];
}

function clientFor(userName, engine) {
  return clients().find((item) => item.userName === userName && item.engine === engine);
}

function availabilities() {
  return state.availability?.availability || [];
}

function availabilityForTunnel(id) {
  return availabilities().find((item) => item.tunnelId === id);
}

function userOnlineBadges(user) {
  const items = [];
  if (user.hasFrpToken) items.push(clientBadge(clientFor(user.name, "frp"), "FRP"));
  if (user.hasNpsVerifyKey) items.push(clientBadge(clientFor(user.name, "nps"), "NPS"));
  return items.length ? `<div class="client-stack">${items.join("")}</div>` : "-";
}

function tunnelClientBadge(tunnel) {
  if (!tunnel.enabled) return '<span class="badge idle">停用</span>';
  const status = clientFor(tunnel.userName, tunnel.engine);
  return clientBadge(status, tunnel.engine.toUpperCase());
}

function tunnelAvailabilityBadge(tunnel) {
  if (!tunnel.enabled) return '<span class="badge idle">停用</span>';
  const availability = availabilityForTunnel(tunnel.id);
  if (!availability) return tunnelClientBadge(tunnel);
  const labels = {
    ok: "可用",
    warning: "注意",
    down: "异常",
    unknown: "未知",
    disabled: "停用",
  };
  const classes = {
    ok: "ok",
    warning: "warn",
    down: "danger",
    unknown: "idle",
    disabled: "idle",
  };
  const entry = availability.entry || {};
  const detail = [
    availability.message || "",
    availability.clientState ? `客户端: ${availability.clientState}` : "",
    entry.target ? `入口: ${entry.target}` : "",
    entry.message ? `检测: ${entry.message}` : "",
    entry.statusCode ? `HTTP: ${entry.statusCode}` : "",
    Number.isFinite(entry.latencyMs) && entry.latencyMs > 0 ? `耗时: ${entry.latencyMs}ms` : "",
    availability.checkedAt ? `时间: ${new Date(availability.checkedAt).toLocaleString()}` : "",
  ].filter(Boolean).join("\n");
  return `<span class="badge ${classes[availability.state] || "idle"}" title="${escapeHtml(detail)}">${labels[availability.state] || "未知"}</span>`;
}

function clientBadge(status, label) {
  if (!status) return `<span class="badge idle">${escapeHtml(label)} 未知</span>`;
  const cls = status.state === "online" ? "ok" : status.state === "offline" ? "danger" : "idle";
  const text = status.state === "online" ? "在线" : status.state === "offline" ? "离线" : "未知";
  const detail = [
    status.clientIp ? `IP: ${status.clientIp}` : "",
    status.hostname ? `主机: ${status.hostname}` : "",
    status.version ? `版本: ${status.version}` : "",
    status.lastSeenAt ? `最近: ${new Date(status.lastSeenAt).toLocaleString()}` : "",
    `隧道: ${status.tunnelOnline}/${status.tunnelTotal}`,
  ].filter(Boolean).join("\n");
  return `<span class="badge ${cls}" title="${escapeHtml(detail)}">${escapeHtml(label)} ${text}</span>`;
}

function resourceMeter(label, used, total) {
  const unlimited = total < 0;
  const percent = unlimited || total === 0 ? 0 : Math.min(100, Math.round((used / total) * 100));
  const totalText = unlimited ? "不限" : total;
  return `
    <div class="resource-meter">
      <div class="resource-meter-label">
        <span>${escapeHtml(label)}</span>
        <span>${used}/${totalText}</span>
      </div>
      <div class="resource-bar"><span style="width: ${percent}%"></span></div>
    </div>
  `;
}

function renderTunnels() {
  $("#tunnels-table").innerHTML = state.tunnels.map((t) => `
    <tr>
      <td title="${escapeHtml(t.id)}">${escapeHtml(t.id)}</td>
      <td>${escapeHtml(t.userName)}</td>
      <td>${engineBadge(t.engine)}</td>
      <td>${escapeHtml(t.mode)}</td>
      <td title="${escapeHtml(tunnelEntry(t))}">${escapeHtml(tunnelEntry(t))}</td>
      <td>${escapeHtml(`${t.localIp || "-"}:${t.localPort || "-"}`)}</td>
      <td>${tunnelAvailabilityBadge(t)}</td>
      <td>${statusBadge(t.enabled)}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-edit-tunnel="${escapeHtml(t.id)}">编辑</button>
          <button class="button small danger" data-delete-tunnel="${escapeHtml(t.id)}">删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(9);
}

function renderEngines() {
  if (!isAdmin()) return;
  const embedded = Boolean(state.runtime?.engines?.embedded);
  $("#engines-table").innerHTML = state.engines.map((e) => `
    <tr>
      <td>${engineBadge(e.engine)}</td>
      <td>${e.configured ? '<span class="badge ok">已配置</span>' : '<span class="badge warn">未配置</span>'}</td>
      <td>${e.running ? `<span class="badge ok">运行${e.pid ? ` PID ${e.pid}` : ""}</span>` : '<span class="badge warn">未运行</span>'}</td>
      <td>${e.port ? `${e.port} ${e.portOpen ? '<span class="badge ok">open</span>' : '<span class="badge warn">closed</span>'}` : "-"}</td>
    </tr>
  `).join("") || emptyRow(4);
  const client = state.runtime?.client || {};
  const engineCfg = state.runtime?.engines || {};
  $("#engine-help").textContent = [
    `运行模式: ${embedded ? "单容器内置 FRP/NPS" : "外部进程"}`,
    `公网地址: ${client.serverAddr || "-"}`,
    "",
    "FRP:",
    `  客户端连接端口: ${client.frpServerPort || "-"}`,
    `  HTTP 入口端口: ${client.frpHttpPort || "-"}`,
    `  HTTPS 入口端口: ${client.frpHttpsPort || "-"}`,
    `  用户文件: ${state.runtime?.frpUsersPath || "-"}`,
    "",
    "NPS:",
    `  NPC 连接端口: ${client.npsServerPort || "-"}`,
    `  HTTP 入口端口: ${client.npsHttpProxyPort || "-"}`,
    `  HTTPS 入口端口: ${client.npsHttpsProxyPort || "-"}`,
    `  客户端文件: ${state.runtime?.npsClientsPath || "-"}`,
    "",
    `配置导出目录: ${state.runtime?.configOutDir || "-"}`,
    "",
    embedded
      ? "内置模式下引擎由容器生命周期管理；修改端口需要重建或重启容器。"
      : `外部 FRP: ${engineCfg.frpsBin || "(未配置)"}\n外部 NPS: ${engineCfg.npsBin || "(未配置)"}`,
  ].join("\n");
}

function renderLogs() {
  if (!isAdmin()) return;
  const logs = state.logs || {};
  const generatedAt = logs.generatedAt ? new Date(logs.generatedAt).toLocaleString() : "-";
  $("#logs-time").textContent = `更新 ${generatedAt}`;
  const entries = logs.entries || [];
  $("#logs-list").innerHTML = entries.length ? entries.map((entry) => `
    <div class="log-entry">
      <span class="log-time">${escapeHtml(new Date(entry.time).toLocaleString())}</span>
      <span class="badge idle">${escapeHtml(entry.stream || "process")}</span>
      <span class="log-message">${escapeHtml(entry.message)}</span>
    </div>
  `).join("") : '<div class="muted-box">暂无日志</div>';
  const box = $("#logs-list");
  box.scrollTop = box.scrollHeight;
}

async function loadLogs(updateState = true) {
  const q = encodeURIComponent($("#logs-query")?.value || "");
  const logs = await api(`/api/logs?limit=200${q ? `&q=${q}` : ""}`);
  if (updateState) {
    state.logs = logs;
    renderLogs();
  }
  return logs;
}

async function clearLogs() {
  if (!confirm("确定清空当前日志缓存？")) return;
  await api("/api/logs", { method: "DELETE" });
  await loadLogs(true);
  toast("日志已清空");
}

function renderUserOptions() {
  const options = state.users.map((u) => `<option value="${escapeHtml(u.name)}">${escapeHtml(u.name)}</option>`).join("");
  const tunnelUser = $('select[name="userName"]');
  tunnelUser.innerHTML = options;
  $("#config-user").innerHTML = options;
  tunnelUser.disabled = !isAdmin();
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
  if ((view === "users" || view === "engines" || view === "logs") && !isAdmin()) return;
  state.view = view;
  $$(".nav-item").forEach((btn) => btn.classList.toggle("active", btn.dataset.view === view));
  $$(".view").forEach((el) => el.classList.toggle("active", el.id === `view-${view}`));
  $("#page-title").textContent = titles[view][0];
  $("#page-subtitle").textContent = titles[view][1];
  render();
  refresh().catch((err) => toast(err.message));
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
  const domainMode = isDomainMode(mode);
  field(form, "remotePort").disabled = domainMode;
  field(form, "remotePort").required = !domainMode;
  field(form, "domains").disabled = !domainMode;
  field(form, "domains").required = domainMode;
  $("#mode-note").textContent = domainMode
    ? "HTTP/HTTPS 不占用用户端口池，必须填写已分配域名；域名 DNS 需要指向服务器。"
    : "TCP/SOCKS5 与 UDP 按协议分别占用端口；远程端口必须在用户端口池内。";
  if (domainMode) {
    field(form, "remotePort").value = "";
  } else {
    field(form, "domains").value = "";
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
    domains: isDomainMode(data.mode) ? data.domains.split(",").map((v) => v.trim()).filter(Boolean) : [],
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

async function exportConfigs() {
  if (!isAdmin()) return;
  const result = await api("/api/export/configs", { method: "POST" });
  toast(`已导出到 ${result.dir}`);
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
});

$("#login-form").addEventListener("submit", (event) => login(event).catch((err) => toast(err.message)));
$("#logout-button").addEventListener("click", () => logout().catch((err) => toast(err.message)));
$("#password-button").addEventListener("click", () => changeOwnPassword().catch((err) => toast(err.message)));
$("#refresh-button").addEventListener("click", () => refresh().then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#refresh-engines").addEventListener("click", () => refresh().then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#user-form").addEventListener("submit", (event) => saveUser(event).catch((err) => toast(err.message)));
$("#tunnel-form").addEventListener("submit", (event) => saveTunnel(event).catch((err) => toast(err.message)));
$('select[name="mode"]').addEventListener("change", updateTunnelModeFields);
$("#clear-user-form").addEventListener("click", clearUserForm);
$("#clear-tunnel-form").addEventListener("click", clearTunnelForm);
$("#load-frpc").addEventListener("click", () => loadConfig("frp").catch((err) => toast(err.message)));
$("#load-npc").addEventListener("click", () => loadConfig("nps").catch((err) => toast(err.message)));
$("#copy-config").addEventListener("click", () => copyConfig().catch((err) => toast(err.message)));
$("#export-configs").addEventListener("click", () => exportConfigs().catch((err) => toast(err.message)));
$("#refresh-logs").addEventListener("click", () => loadLogs(true).then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#clear-logs").addEventListener("click", () => clearLogs().catch((err) => toast(err.message)));
$("#logs-query").addEventListener("keydown", (event) => {
  if (event.key === "Enter") loadLogs(true).catch((err) => toast(err.message));
});

boot();
updateTunnelModeFields();
