const state = {
  me: null,
  users: [],
  nodes: [],
  tunnels: [],
  runtime: null,
  diagnostics: null,
  clients: null,
  availability: null,
  availabilityKey: "",
  logs: null,
  view: "dashboard",
};

const titles = {
  dashboard: ["概览", "统一用户、端口池、域名池和双引擎隧道。"],
  users: ["用户", "管理员创建账号，分配固定端口池和域名池。"],
  nodes: ["节点", "管理承载 NPS 引擎的服务器节点。"],
  tunnels: ["隧道", "创建 NPS 隧道；TCP/UDP/SOCKS5 用端口，HTTP/HTTPS 用域名。"],
  configs: ["配置", "查看统一客户端、npc 启动命令和 NPS 密钥。"],
  logs: ["日志", "查看总控和节点同步相关的最近运行日志。"],
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => Array.from(document.querySelectorAll(selector));

function setHTML(target, html) {
  const el = typeof target === "string" ? $(target) : target;
  if (el && el.innerHTML !== html) el.innerHTML = html;
}

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
  if (state.view === "dashboard" || state.view === "nodes" || state.view === "tunnels" || state.view === "configs") {
    requests.nodes = api("/api/nodes");
  }
  if (state.view === "dashboard" || state.view === "users" || state.view === "tunnels") {
    requests.clients = api("/api/clients");
  }
  if (isAdmin() && state.view === "logs") {
    requests.logs = loadLogs(false);
  }
  const entries = await Promise.all(Object.entries(requests).map(async ([key, request]) => [key, await request]));
  for (const [key, value] of entries) {
    if (key === "runtime") state.runtime = value;
    if (key === "users") state.users = value;
    if (key === "nodes") state.nodes = value;
    if (key === "tunnels") state.tunnels = value;
    if (key === "diagnostics") state.diagnostics = value;
    if (key === "clients") state.clients = value;
    if (key === "logs") state.logs = value;
  }
  render();

  // 异步获取可用性状态，不阻塞页面基础渲染
  if (state.view === "dashboard" || state.view === "tunnels") {
    api("/api/availability")
      .then((value) => {
        const key = JSON.stringify(value?.availability || []);
        if (key === state.availabilityKey) return;
        state.availabilityKey = key;
        state.availability = value;
        render();
      })
      .catch((err) => console.error("可用性检测失败:", err));
  }
}

function render() {
  $("#session-user").textContent = `${state.me.name} (${state.me.role})`;
  $$(".admin-only").forEach((el) => el.classList.toggle("hidden", !isAdmin()));
  if (!isAdmin() && (state.view === "users" || state.view === "nodes" || state.view === "logs")) {
    switchView("dashboard");
  }
  renderUserOptions();
  renderNodeOptions();
  if (state.view === "dashboard") {
    renderMetrics();
    renderDashboardTables();
    renderDiagnostics();
  }
  if (state.view === "users") {
    renderUsers();
    renderDiagnostics();
  }
  if (state.view === "nodes") renderNodes();
  if (state.view === "tunnels") {
    renderTunnels();
    updateTunnelModeFields();
  }
  if (state.view === "logs") renderLogs();
}

function renderMetrics() {
  $("#metric-users").textContent = state.users.length;
  $("#metric-tunnels").textContent = state.tunnels.length;
  $("#metric-nodes").textContent = nodes().filter((node) => node.id !== "local").length;
  $("#metric-online-nodes").textContent = nodes().filter((node) => node.id !== "local" && node.status?.online).length;
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
      <td>${userTrafficSummary(u)}</td>
      <td>${expirySummary(u)}</td>
      <td>
        <span class="badge ${u.hasNpsVerifyKey ? "ok" : "warn"}">NPS</span>
      </td>
      <td>${userOnlineBadges(u)}</td>
      <td>${statusBadge(u.enabled)}</td>
      <td>
        <div class="cell-actions">
          <button class="button small secondary" data-edit-user="${escapeHtml(u.name)}">编辑</button>
          <button class="button small secondary" data-reset-user-flow="${escapeHtml(u.name)}">清零流量</button>
          <button class="button small danger" data-delete-user="${escapeHtml(u.name)}">删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(11);
}

function nodes() {
  const values = state.nodes.length ? state.nodes : state.runtime?.nodes || [];
  return values.length ? values : [{ id: "local", name: "Local Node", enabled: true, frpEnabled: false, npsEnabled: true }];
}

function nodeFor(id) {
  const nodeId = id || "local";
  return nodes().find((node) => node.id === nodeId);
}

function nodeLabel(id) {
  const node = nodeFor(id);
  if (!node) return id || "local";
  return node.name && node.name !== node.id ? `${node.name} (${node.id})` : node.id;
}

function renderNodes() {
  if (!isAdmin()) return;
  $("#nodes-table").innerHTML = nodes().map((node) => `
    <tr>
      <td>
        <strong>${escapeHtml(node.name || node.id)}</strong>
        <div class="muted-line">${escapeHtml(node.id)}</div>
      </td>
      <td>${escapeHtml(node.publicAddr || "-")}</td>
      <td title="${escapeHtml(node.token || "")}">${escapeHtml(node.token ? `${node.token.slice(0, 8)}...` : "-")}</td>
      <td>
        <span class="badge ${node.npsEnabled ? "ok" : "idle"}">NPS</span>
      </td>
      <td title="${escapeHtml(formatPools(node.portPools))}">${escapeHtml(formatPools(node.portPools))}</td>
      <td title="${escapeHtml(formatDomains(node.domainPools))}">${escapeHtml(formatDomains(node.domainPools))}</td>
      <td>${statusBadge(node.enabled)}</td>
      <td>${nodeSyncBadge(node)}</td>
      <td>
        <div class="cell-actions">
          ${node.id !== "local" ? `<button class="button small" data-deploy-node="${escapeHtml(node.id)}" data-node-token="${escapeHtml(node.token)}">部署</button>` : ""}
          <button class="button small secondary" data-edit-node="${escapeHtml(node.id)}">编辑</button>
          <button class="button small danger" data-delete-node="${escapeHtml(node.id)}" ${node.id === "local" ? "disabled" : ""}>删除</button>
        </div>
      </td>
    </tr>
  `).join("") || emptyRow(9);
}

function renderDiagnostics() {
  const diagnostics = state.diagnostics || {};
  const generatedAt = diagnostics.generatedAt ? new Date(diagnostics.generatedAt).toLocaleString() : "-";
  $("#diagnostics-time").textContent = `更新 ${generatedAt}`;
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
      <td>${escapeHtml(nodeLabel(t.nodeId))}</td>
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
  `).join("") || emptyRow(10);
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
  setHTML(tunnelUser, options);
  setHTML("#config-user", options);
  tunnelUser.disabled = !isAdmin();
}

function renderNodeOptions() {
  const options = nodes().map((node) => `<option value="${escapeHtml(node.id)}">${escapeHtml(nodeLabel(node.id))}</option>`).join("");
  const tunnelNode = $('select[name="nodeId"]');
  if (tunnelNode) setHTML(tunnelNode, options);
  const configNode = $("#config-node");
  if (configNode) setHTML(configNode, options);
}

function statusBadge(enabled) {
  return `<span class="badge ${enabled ? "ok" : "warn"}">${enabled ? "启用" : "停用"}</span>`;
}

function nodeSyncBadge(node) {
  if (node.id === "local") return '<span class="badge idle">总控</span>';
  const status = node.status || {};
  const online = Boolean(status.online);
  const lastSeen = status.lastSeenAt ? new Date(status.lastSeenAt).toLocaleString() : "";
  const lastSync = status.lastSyncAt ? new Date(status.lastSyncAt).toLocaleString() : "";
  const detail = [
    online ? "最近推送成功" : "最近推送失败或尚未同步",
    lastSeen ? `在线时间: ${lastSeen}` : "",
    lastSync ? `同步时间: ${lastSync}` : "",
    status.lastError ? `错误: ${status.lastError}` : "",
  ].filter(Boolean).join("\n");
  return `<span class="badge ${online ? "ok" : "danger"}" title="${escapeHtml(detail)}">${online ? "在线" : "离线"}</span>`;
}

function engineBadge(engine) {
  return `<span class="badge engine-${escapeHtml(engine)}">${escapeHtml(engine).toUpperCase()}</span>`;
}

function emptyRow(cols) {
  return `<tr><td colspan="${cols}">暂无数据</td></tr>`;
}

function switchView(view) {
  if ((view === "users" || view === "nodes" || view === "logs") && !isAdmin()) return;
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
  const form = $("#user-form");
  field(form, "name").readOnly = false;
  $("#user-form-title").textContent = "创建用户";
  $("#user-submit-btn").textContent = "创建用户";
  $('#user-form input[name="enabled"]').checked = true;
  $('#user-form select[name="role"]').value = "user";
  $('#user-form input[name="maxPorts"]').value = "3";
  $('#user-form input[name="rateLimit"]').value = "0";
  $('#user-form input[name="flowLimit"]').value = "0";
  $('#user-form input[name="expiresAt"]').value = "";
}

function clearNodeForm() {
  $("#node-form").reset();
  const form = $("#node-form");
  field(form, "id").readOnly = false;
  $("#node-form-title").textContent = "创建节点";
  $("#node-submit-btn").textContent = "创建节点";
  $('#node-form input[name="enabled"]').checked = true;
  $('#node-form input[name="npsEnabled"]').checked = true;
}

function clearTunnelForm() {
  $("#tunnel-form").reset();
  const form = $("#tunnel-form");
  $("#tunnel-form-title").textContent = "创建隧道";
  $("#tunnel-submit-btn").textContent = "创建隧道";
  $('#tunnel-form input[name="id"]').value = "";
  $('#tunnel-form input[name="localIp"]').value = "127.0.0.1";
  $('#tunnel-form input[name="enabled"]').checked = true;
  if (state.users[0]) $('select[name="userName"]').value = state.users[0].name;
  if (nodes()[0]) $('select[name="nodeId"]').value = nodes()[0].id;
  updateTunnelModeFields();
}

function editUser(name) {
  if (!isAdmin()) return;
  const user = state.users.find((u) => u.name === name);
  if (!user) return;
  const form = $("#user-form");
  field(form, "name").value = user.name;
  field(form, "name").readOnly = true;
  $("#user-form-title").textContent = `编辑用户: ${user.name}`;
  $("#user-submit-btn").textContent = "保存修改";
  field(form, "password").value = "";
  field(form, "role").value = user.role;
  field(form, "maxPorts").value = user.maxPorts || 0;
  field(form, "portPool").value = formatPools(user.portPools);
  field(form, "domainPool").value = formatDomains(user.domainPools);
  field(form, "npsVerifyKey").value = user.npsVerifyKey || "";
  field(form, "enabled").checked = user.enabled;
  field(form, "rateLimit").value = user.rateLimit || 0;
  field(form, "flowLimit").value = user.flowLimit || 0;
  field(form, "expiresAt").value = toDateTimeLocal(user.expiresAt);
  switchView("users");
}

function editNode(id) {
  if (!isAdmin()) return;
  const node = nodeFor(id);
  if (!node) return;
  const form = $("#node-form");
  const runtime = node.runtime || {};
  field(form, "id").value = node.id;
  field(form, "id").readOnly = true;
  $("#node-form-title").textContent = `编辑节点: ${node.id}`;
  $("#node-submit-btn").textContent = "保存修改";
  field(form, "name").value = node.name || "";
  field(form, "token").value = node.token || "";
  field(form, "publicAddr").value = node.publicAddr || "";
  field(form, "portPool").value = formatPools(node.portPools);
  field(form, "domainPool").value = formatDomains(node.domainPools);
  field(form, "npsServerPort").value = runtime.npsServerPort || "";
  field(form, "npsHttpProxyPort").value = runtime.npsHttpProxyPort || "";
  field(form, "npsHttpsProxyPort").value = runtime.npsHttpsProxyPort || "";
  field(form, "enabled").checked = node.enabled;
  field(form, "npsEnabled").checked = node.npsEnabled;
  switchView("nodes");
}

function editTunnel(id) {
  const tunnel = state.tunnels.find((t) => t.id === id);
  if (!tunnel) return;
  const form = $("#tunnel-form");
  field(form, "id").value = tunnel.id;
  $("#tunnel-form-title").textContent = `编辑隧道: ${tunnel.id}`;
  $("#tunnel-submit-btn").textContent = "保存修改";
  field(form, "userName").value = tunnel.userName;
  field(form, "nodeId").value = tunnel.nodeId || "local";
  if (field(form, "engine")) field(form, "engine").value = tunnel.engine;
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
    npsVerifyKey: data.npsVerifyKey.trim(),
    rateLimit: Number(data.rateLimit || 0),
    flowLimit: Number(data.flowLimit || 0),
    expiresAt: fromDateTimeLocal(data.expiresAt),
  };
  await api("/api/users", { method: "POST", body: JSON.stringify(body) });
  clearUserForm();
  await refresh();
  toast("用户已保存");
}

async function saveNode(event) {
  event.preventDefault();
  if (!isAdmin()) return;
  const form = event.currentTarget;
  const data = formData(form);
  const body = {
    id: data.id.trim(),
    name: data.name.trim(),
    token: data.token.trim(),
    publicAddr: data.publicAddr.trim(),
    enabled: field(form, "enabled").checked,
    npsEnabled: field(form, "npsEnabled").checked,
    portPool: data.portPool.trim(),
    domainPool: data.domainPool.trim(),
    runtime: {
      npsServerPort: Number(data.npsServerPort || 0),
      npsHttpProxyPort: Number(data.npsHttpProxyPort || 0),
      npsHttpsProxyPort: Number(data.npsHttpsProxyPort || 0),
    },
  };
  await api("/api/nodes", { method: "POST", body: JSON.stringify(body) });
  clearNodeForm();
  await refresh();
  toast("节点已保存");
}

async function saveTunnel(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const data = formData(form);
  const body = {
    id: data.id,
    userName: isAdmin() ? data.userName : state.me.name,
    nodeId: data.nodeId || "local",
    engine: "nps",
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

async function resetUserFlow(name) {
  if (!isAdmin()) return;
  if (!confirm(`清零用户 ${name} 的已用流量？`)) return;
  await api(`/api/users/${encodeURIComponent(name)}/reset-flow`, { method: "POST" });
  await refresh();
  toast("流量已清零");
}

async function deleteNode(id) {
  if (!isAdmin()) return;
  if (!confirm(`删除节点 ${id}？已被隧道使用的节点不能删除。`)) return;
  await api(`/api/nodes/${encodeURIComponent(id)}`, { method: "DELETE" });
  await refresh();
  toast("节点已删除");
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
  const paths = {
    nps: "npc-command",
    tunnelClient: "tunnel-client",
    npsKey: "nps-key",
  };
  const path = paths[kind] || "npc-command";
  const node = $("#config-node")?.value || "local";
  const text = await api(`/api/users/${encodeURIComponent(user)}/${path}?node=${encodeURIComponent(node)}`, { headers: {} });
  $("#config-title").textContent = kind === "npsKey" ? "NPS 密钥" : kind === "tunnelClient" ? "Cosmo NPS Client" : "npc";
  $("#config-output").textContent = text;
}

async function copyConfig() {
  const text = $("#config-output").textContent;
  if (!text) return;
  await copyText(text);
  toast("已复制");
}

async function copyText(text) {
  if (typeof navigator !== "undefined" && navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
    await navigator.clipboard.writeText(text);
    return;
  }
  const input = document.createElement("textarea");
  input.value = text;
  input.setAttribute("readonly", "");
  input.style.position = "fixed";
  input.style.left = "-9999px";
  input.style.top = "0";
  document.body.appendChild(input);
  input.focus();
  input.select();
  try {
    if (!document.execCommand("copy")) {
      throw new Error("copy command was rejected");
    }
  } finally {
    document.body.removeChild(input);
  }
}

async function exportConfigs() {
  if (!isAdmin()) return;
  const result = await api("/api/export/configs", { method: "POST" });
  toast(`已导出到 ${result.dir}`);
}

function openDeployModal(id, token) {
  const origin = window.location.origin;
  const cmd = `curl -fsSL "${origin}/api/agent/bootstrap?id=${encodeURIComponent(id)}&token=${encodeURIComponent(token)}" | bash`;
  const downloadLink = `${origin}/api/agent/download`;
  
  $("#deploy-cmd-text").textContent = cmd;
  $("#manual-download-link").href = downloadLink;
  $("#deploy-modal").classList.add("show");
}

function closeDeployModal() {
  $("#deploy-modal").classList.remove("show");
}

async function copyDeployCommand() {
  const text = $("#deploy-cmd-text").textContent;
  if (!text) return;
  await copyText(text);
  toast("部署命令已复制到剪贴板");
}

document.addEventListener("click", async (event) => {
  const target = event.target;
  if (!(target instanceof HTMLElement)) return;
  if (target.dataset.deployNode) openDeployModal(target.dataset.deployNode, target.dataset.nodeToken);
  if (target.dataset.view) switchView(target.dataset.view);
  if (target.dataset.jump) switchView(target.dataset.jump);
  if (target.dataset.editUser) editUser(target.dataset.editUser);
  if (target.dataset.editNode) editNode(target.dataset.editNode);
  if (target.dataset.editTunnel) editTunnel(target.dataset.editTunnel);
  if (target.dataset.resetUserFlow) await resetUserFlow(target.dataset.resetUserFlow).catch((err) => toast(err.message));
  if (target.dataset.deleteUser) await deleteUser(target.dataset.deleteUser).catch((err) => toast(err.message));
  if (target.dataset.deleteNode) await deleteNode(target.dataset.deleteNode).catch((err) => toast(err.message));
  if (target.dataset.deleteTunnel) await deleteTunnel(target.dataset.deleteTunnel).catch((err) => toast(err.message));
});

$("#login-form").addEventListener("submit", (event) => login(event).catch((err) => toast(err.message)));
$("#logout-button").addEventListener("click", () => logout().catch((err) => toast(err.message)));
$("#password-button").addEventListener("click", () => changeOwnPassword().catch((err) => toast(err.message)));
$("#refresh-button").addEventListener("click", () => refresh().then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#user-form").addEventListener("submit", (event) => saveUser(event).catch((err) => toast(err.message)));
$("#node-form").addEventListener("submit", (event) => saveNode(event).catch((err) => toast(err.message)));
$("#tunnel-form").addEventListener("submit", (event) => saveTunnel(event).catch((err) => toast(err.message)));
$('select[name="mode"]').addEventListener("change", updateTunnelModeFields);
$("#clear-user-form").addEventListener("click", clearUserForm);
$("#clear-node-form").addEventListener("click", clearNodeForm);
$("#clear-tunnel-form").addEventListener("click", clearTunnelForm);
$("#load-npc").addEventListener("click", () => loadConfig("nps").catch((err) => toast(err.message)));
$("#load-tunnel-client").addEventListener("click", () => loadConfig("tunnelClient").catch((err) => toast(err.message)));
$("#load-nps-key").addEventListener("click", () => loadConfig("npsKey").catch((err) => toast(err.message)));
$("#copy-config").addEventListener("click", () => copyConfig().catch((err) => toast(err.message)));
$("#export-configs").addEventListener("click", () => exportConfigs().catch((err) => toast(err.message)));
$("#refresh-logs").addEventListener("click", () => loadLogs(true).then(() => toast("已刷新")).catch((err) => toast(err.message)));
$("#clear-logs").addEventListener("click", () => clearLogs().catch((err) => toast(err.message)));
$("#logs-query").addEventListener("keydown", (event) => {
  if (event.key === "Enter") loadLogs(true).catch((err) => toast(err.message));
});

$("#close-deploy-modal").addEventListener("click", closeDeployModal);
$("#copy-deploy-cmd").addEventListener("click", () => copyDeployCommand().catch((err) => toast(err.message)));
$("#deploy-modal").addEventListener("click", (event) => {
  if (event.target === event.currentTarget) closeDeployModal();
});

function formatBytes(bytes) {
  if (bytes === 0 || !bytes) return "0 B";
  if (bytes < 1024) return bytes + " B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

function formatSpeed(bytesPerSec) {
  if (!bytesPerSec || bytesPerSec <= 0) return "静默";
  if (bytesPerSec < 1024) return bytesPerSec + " B/s";
  const k = 1024;
  const sizes = ["B/s", "KB/s", "MB/s", "GB/s"];
  const i = Math.floor(Math.log(bytesPerSec) / Math.log(k));
  return parseFloat((bytesPerSec / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function toDateTimeLocal(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (num) => String(num).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function fromDateTimeLocal(value) {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

function expirySummary(u) {
  if (!u.expiresAt) return `<span class="badge idle">永不过期</span>`;
  const expiresAt = new Date(u.expiresAt);
  if (Number.isNaN(expiresAt.getTime())) return `<span class="badge warn">时间异常</span>`;
  const expired = expiresAt.getTime() <= Date.now();
  const text = expiresAt.toLocaleString();
  if (expired) {
    const deleteHint = u.expiredAt ? `，预计 ${new Date(new Date(u.expiredAt).getTime() + 7 * 24 * 60 * 60 * 1000).toLocaleString()} 后清理` : "";
    return `<span class="badge danger" title="已到期${deleteHint}">已到期</span><div class="muted tiny">${escapeHtml(text)}</div>`;
  }
  return `<span class="badge ok">有效</span><div class="muted tiny">${escapeHtml(text)}</div>`;
}

function userTrafficSummary(u) {
  const rateLimitText = u.rateLimit > 0 ? `${u.rateLimit} Mbps` : "不限速";
  const flowUsedText = formatBytes(u.flowUsed || 0);
  const upSpeed = formatSpeed(u.inletSpeed || 0);
  const downSpeed = formatSpeed(u.exportSpeed || 0);
  let speedText = "静默";
  if (u.inletSpeed > 0 || u.exportSpeed > 0) {
    const upStr = u.inletSpeed > 0 ? `↑${upSpeed}` : "";
    const downStr = u.exportSpeed > 0 ? `↓${downSpeed}` : "";
    speedText = [upStr, downStr].filter(Boolean).join(" ");
  }
  const speedClass = speedText === "静默" ? "speed-silent" : "speed-active";
  
  if (u.flowLimit > 0) {
    const limitBytes = u.flowLimit * 1024 * 1024 * 1024;
    const percent = Math.min(100, Math.round((u.flowUsed || 0) / limitBytes * 100));
    return `
      <div class="traffic-summary">
        <div class="traffic-limits">
          <span class="badge ok">${rateLimitText}</span>
          <span class="traffic-total">${flowUsedText} / ${u.flowLimit} GB</span>
        </div>
        <div class="traffic-progress-bar" title="已用 ${percent}%">
          <div class="bar-fill" style="width: ${percent}%;"></div>
        </div>
        <span class="traffic-speed ${speedClass}">${speedText}</span>
      </div>
    `;
  } else {
    return `
      <div class="traffic-summary">
        <div class="traffic-limits">
          <span class="badge ok">${rateLimitText}</span>
          <span class="traffic-total">${flowUsedText} / 不限</span>
        </div>
        <span class="traffic-speed ${speedClass}">${speedText}</span>
      </div>
    `;
  }
}

boot();
updateTunnelModeFields();
