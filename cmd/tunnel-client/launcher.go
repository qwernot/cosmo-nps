package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type launcherConfig struct {
	ControlURL string `json:"controlUrl"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	Refresh    string `json:"refresh"`
}

type launcherState struct {
	mu      sync.Mutex
	cfg     launcherConfig
	cmd     *exec.Cmd
	running bool
	started time.Time
	logs    []string
}

func runLauncher(addr, controlURL string, refresh time.Duration) error {
	cfg := loadLauncherConfig()
	if cfg.ControlURL == "" {
		cfg.ControlURL = controlURL
	}
	if cfg.Refresh == "" {
		cfg.Refresh = refresh.String()
	}
	state := &launcherState{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(launcherHTML))
	})
	mux.HandleFunc("GET /api/status", state.status)
	mux.HandleFunc("POST /api/start", state.start)
	mux.HandleFunc("POST /api/stop", state.stop)

	url := "http://" + addr
	log.Printf("tunnel-client launcher listening on %s", url)
	go openBrowser(url)
	return http.ListenAndServe(addr, mux)
}

func (s *launcherState) status(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeLauncherJSON(w, map[string]any{
		"config":  s.cfg,
		"running": s.running,
		"started": s.started,
		"logs":    append([]string(nil), s.logs...),
	})
}

func (s *launcherState) start(w http.ResponseWriter, r *http.Request) {
	var cfg launcherConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeLauncherError(w, http.StatusBadRequest, err)
		return
	}
	cfg.ControlURL = strings.TrimSpace(cfg.ControlURL)
	cfg.User = strings.TrimSpace(cfg.User)
	if cfg.Refresh == "" {
		cfg.Refresh = "30s"
	}
	if cfg.ControlURL == "" || cfg.User == "" || cfg.Password == "" {
		writeLauncherError(w, http.StatusBadRequest, fmt.Errorf("server, user and password are required"))
		return
	}
	if _, err := time.ParseDuration(cfg.Refresh); err != nil {
		writeLauncherError(w, http.StatusBadRequest, fmt.Errorf("invalid refresh interval: %w", err))
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusConflict, fmt.Errorf("client is already running"))
		return
	}
	exe, err := os.Executable()
	if err != nil {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusInternalServerError, err)
		return
	}
	cmd := exec.Command(exe,
		"-no-gui",
		"-server", cfg.ControlURL,
		"-user", cfg.User,
		"-password", cfg.Password,
		"-refresh", cfg.Refresh,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusInternalServerError, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusInternalServerError, err)
		return
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusInternalServerError, err)
		return
	}
	s.cfg = cfg
	s.cmd = cmd
	s.running = true
	s.started = time.Now()
	s.appendLocked("launcher: tunnel-client started")
	s.mu.Unlock()

	_ = saveLauncherConfig(cfg)
	go s.capture(stdout)
	go s.capture(stderr)
	go s.wait(cmd)
	writeLauncherJSON(w, map[string]any{"status": "started"})
}

func (s *launcherState) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cmd := s.cmd
	if !s.running || cmd == nil || cmd.Process == nil {
		s.mu.Unlock()
		writeLauncherJSON(w, map[string]any{"status": "stopped"})
		return
	}
	s.appendLocked("launcher: stopping tunnel-client")
	s.mu.Unlock()

	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	writeLauncherJSON(w, map[string]any{"status": "stopping"})
}

func (s *launcherState) capture(pipe any) {
	reader, ok := pipe.(*os.File)
	if !ok {
		return
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		s.mu.Lock()
		s.appendLocked(scanner.Text())
		s.mu.Unlock()
	}
}

func (s *launcherState) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == cmd {
		s.running = false
		s.cmd = nil
	}
	if err != nil {
		s.appendLocked("launcher: tunnel-client exited: " + err.Error())
	} else {
		s.appendLocked("launcher: tunnel-client stopped")
	}
}

func (s *launcherState) appendLocked(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	s.logs = append(s.logs, time.Now().Format("15:04:05")+" "+line)
	if len(s.logs) > 300 {
		s.logs = s.logs[len(s.logs)-300:]
	}
}

func loadLauncherConfig() launcherConfig {
	var cfg launcherConfig
	b, err := os.ReadFile(launcherConfigPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg.Password = ""
	return cfg
}

func saveLauncherConfig(cfg launcherConfig) error {
	cfg.Password = ""
	path := launcherConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func launcherConfigPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "tunnel-control", "client.json")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		if browser := windowsAppBrowser(); browser != "" {
			cmd = exec.Command(browser, "--app="+url, "--window-size=980,720")
			break
		}
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func windowsAppBrowser() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
	}
	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func writeLauncherJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func writeLauncherError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

const launcherHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Tunnel Client</title>
  <style>
    :root { color-scheme: dark; font-family: "Segoe UI", "Microsoft YaHei", Arial, sans-serif; color: #eef6ff; background: #08111f; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; padding: 18px; background: radial-gradient(circle at 20% 12%, rgba(0, 214, 201, .24), transparent 28%), radial-gradient(circle at 85% 0%, rgba(68, 119, 255, .26), transparent 24%), linear-gradient(145deg, #08111f 0%, #0c1829 45%, #111827 100%); }
    main { width: min(1080px, 100%); margin: 0 auto; display: grid; grid-template-columns: 330px minmax(0, 1fr); gap: 16px; }
    .hero { min-height: 676px; border: 1px solid rgba(125, 211, 252, .22); border-radius: 18px; padding: 24px; background: linear-gradient(155deg, rgba(14, 165, 233, .22), rgba(15, 23, 42, .86) 48%, rgba(20, 184, 166, .16)); box-shadow: 0 28px 80px rgba(0, 0, 0, .38); position: relative; overflow: hidden; }
    .hero::after { content: ""; position: absolute; inset: auto -30px -65px 24px; height: 170px; background: linear-gradient(90deg, rgba(45, 212, 191, .22), rgba(96, 165, 250, .16)); filter: blur(2px); transform: skewY(-8deg); }
    .brand { position: relative; z-index: 1; display: flex; align-items: center; gap: 12px; }
    .mark { width: 44px; height: 44px; border-radius: 14px; display: grid; place-items: center; background: linear-gradient(145deg, #22d3ee, #2563eb); box-shadow: 0 16px 30px rgba(37, 99, 235, .45); font-weight: 900; color: white; }
    h1 { margin: 0; font-size: 25px; letter-spacing: 0; }
    .sub { margin: 4px 0 0; color: #9fb3ca; font-size: 13px; }
    .status-card { position: relative; z-index: 1; margin-top: 34px; padding: 18px; border-radius: 16px; background: rgba(8, 18, 33, .56); border: 1px solid rgba(148, 163, 184, .18); }
    .status { display: inline-flex; align-items: center; gap: 8px; padding: 9px 12px; border-radius: 999px; background: rgba(148, 163, 184, .14); color: #cbd5e1; font-size: 13px; font-weight: 800; }
    .status::before { content: ""; width: 8px; height: 8px; border-radius: 50%; background: #94a3b8; box-shadow: 0 0 0 5px rgba(148, 163, 184, .12); }
    .status.online { background: rgba(34, 197, 94, .16); color: #bbf7d0; }
    .status.online::before { background: #22c55e; box-shadow: 0 0 0 5px rgba(34, 197, 94, .15); }
    .metric { margin-top: 18px; display: grid; gap: 10px; }
    .metric div { display: flex; justify-content: space-between; color: #b6c6d8; font-size: 13px; }
    .metric strong { color: #f8fafc; }
    .panel { border: 1px solid rgba(148, 163, 184, .22); border-radius: 18px; background: rgba(15, 23, 42, .78); box-shadow: 0 24px 70px rgba(0, 0, 0, .32); backdrop-filter: blur(14px); }
    .config { padding: 22px; }
    .panel-head { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 18px; }
    h2 { margin: 0; font-size: 18px; letter-spacing: 0; }
    .hint { color: #94a3b8; font-size: 12px; }
    form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
    label { display: grid; gap: 7px; font-size: 12px; font-weight: 800; color: #cbd5e1; }
    input { width: 100%; border: 1px solid rgba(148, 163, 184, .26); border-radius: 11px; padding: 12px 13px; font: inherit; color: #f8fafc; background: rgba(2, 8, 23, .52); outline: none; }
    input:focus { border-color: rgba(34, 211, 238, .78); box-shadow: 0 0 0 4px rgba(34, 211, 238, .12); }
    .wide { grid-column: 1 / -1; }
    .actions { display: flex; gap: 10px; align-items: center; grid-column: 1 / -1; padding-top: 4px; }
    button { border: 0; border-radius: 11px; padding: 12px 18px; font-weight: 900; cursor: pointer; color: white; background: linear-gradient(135deg, #06b6d4, #2563eb); box-shadow: 0 14px 28px rgba(37, 99, 235, .28); }
    button.secondary { background: rgba(148, 163, 184, .18); color: #dbeafe; box-shadow: none; }
    button.danger { background: linear-gradient(135deg, #fb7185, #dc2626); box-shadow: 0 14px 26px rgba(220, 38, 38, .18); }
    .content { display: grid; gap: 16px; }
    .logs-panel { overflow: hidden; }
    .logs-head { padding: 16px 18px; border-bottom: 1px solid rgba(148, 163, 184, .16); display: flex; align-items: center; justify-content: space-between; }
    pre { margin: 0; min-height: 360px; max-height: 450px; overflow: auto; background: rgba(2, 6, 23, .84); color: #bfdbfe; padding: 16px 18px; font: 12px/1.6 Consolas, "Cascadia Mono", monospace; white-space: pre-wrap; }
    @media (max-width: 880px) { main { grid-template-columns: 1fr; } .hero { min-height: auto; } }
  </style>
</head>
<body>
  <main>
    <aside class="hero">
      <div class="brand">
        <div class="mark">TC</div>
        <div>
          <h1>Tunnel Client</h1>
          <p class="sub">NPS 云穿透客户端</p>
        </div>
      </div>
      <div class="status-card">
        <span class="status" id="status">未运行</span>
        <div class="metric">
          <div><span>总控</span><strong id="server-label">-</strong></div>
          <div><span>账号</span><strong id="user-label">-</strong></div>
          <div><span>模式</span><strong>自动节点</strong></div>
        </div>
      </div>
    </aside>
    <section class="content">
      <section class="panel config">
        <div class="panel-head">
          <h2>连接配置</h2>
          <span class="hint">填写后点击启动</span>
        </div>
        <form id="form">
          <label class="wide">总控地址 <input name="controlUrl" placeholder="http://192.168.6.64:8088" required /></label>
          <label>用户名 <input name="user" required /></label>
          <label>密码 <input name="password" type="password" required /></label>
          <label>刷新间隔 <input name="refresh" value="30s" /></label>
          <div class="actions">
            <button type="submit">启动连接</button>
            <button type="button" class="danger" id="stop">停止</button>
            <button type="button" class="secondary" id="reload">刷新</button>
          </div>
        </form>
      </section>
      <section class="panel logs-panel">
        <div class="logs-head">
          <h2>运行日志</h2>
          <span class="hint">最近 300 条</span>
        </div>
        <pre id="logs">等待启动...</pre>
      </section>
    </section>
  </main>
  <script>
    const form = document.querySelector("#form");
    const statusEl = document.querySelector("#status");
    const logsEl = document.querySelector("#logs");
    const serverLabel = document.querySelector("#server-label");
    const userLabel = document.querySelector("#user-label");
    const api = async (path, options = {}) => {
      const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...options });
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
      return res.json();
    };
    const render = (data) => {
      form.controlUrl.value = data.config?.controlUrl || "";
      form.user.value = data.config?.user || "";
      form.refresh.value = data.config?.refresh || "30s";
      serverLabel.textContent = data.config?.controlUrl || "-";
      userLabel.textContent = data.config?.user || "-";
      statusEl.textContent = data.running ? "运行中" : "未运行";
      statusEl.classList.toggle("online", Boolean(data.running));
      logsEl.textContent = (data.logs || []).join("\n") || "等待启动...";
      logsEl.scrollTop = logsEl.scrollHeight;
    };
    const refresh = async () => render(await api("/api/status"));
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const body = Object.fromEntries(new FormData(form).entries());
      await api("/api/start", { method: "POST", body: JSON.stringify(body) });
      await refresh();
    });
    document.querySelector("#stop").addEventListener("click", async () => { await api("/api/stop", { method: "POST" }); await refresh(); });
    document.querySelector("#reload").addEventListener("click", refresh);
    refresh().catch((err) => logsEl.textContent = err.message);
    setInterval(refresh, 2000);
  </script>
</body>
</html>`
