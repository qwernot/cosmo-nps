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
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
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
    :root { color-scheme: light; font-family: "Segoe UI", Arial, sans-serif; color: #18212f; background: #eef3f8; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px; }
    main { width: min(920px, 100%); display: grid; gap: 16px; }
    .panel { background: rgba(255,255,255,.82); border: 1px solid rgba(148,163,184,.35); border-radius: 12px; box-shadow: 0 18px 45px rgba(15,23,42,.12); padding: 22px; backdrop-filter: blur(12px); }
    h1 { margin: 0 0 4px; font-size: 24px; letter-spacing: 0; }
    p { margin: 0; color: #64748b; }
    form { display: grid; grid-template-columns: repeat(2, minmax(0,1fr)); gap: 14px; margin-top: 18px; }
    label { display: grid; gap: 7px; font-size: 13px; font-weight: 650; color: #334155; }
    input { width: 100%; border: 1px solid #cbd5e1; border-radius: 8px; padding: 11px 12px; font: inherit; background: #fff; }
    .wide { grid-column: 1 / -1; }
    .actions { display: flex; gap: 10px; align-items: center; grid-column: 1 / -1; }
    button { border: 0; border-radius: 8px; padding: 11px 16px; font-weight: 750; cursor: pointer; background: #2563eb; color: white; }
    button.secondary { background: #e2e8f0; color: #1e293b; }
    button.danger { background: #dc2626; }
    .status { margin-left: auto; padding: 7px 10px; border-radius: 999px; background: #e2e8f0; color: #475569; font-size: 13px; }
    .status.online { background: #dcfce7; color: #166534; }
    pre { margin: 0; min-height: 260px; max-height: 420px; overflow: auto; background: #0f172a; color: #dbeafe; border-radius: 10px; padding: 14px; font: 12px/1.55 Consolas, monospace; white-space: pre-wrap; }
    @media (max-width: 720px) { form { grid-template-columns: 1fr; } body { padding: 12px; } }
  </style>
</head>
<body>
  <main>
    <section class="panel">
      <h1>Tunnel Client</h1>
      <p>填写总控账号后启动，客户端会自动连接该用户的 NPS 节点。</p>
      <form id="form">
        <label class="wide">总控地址 <input name="controlUrl" placeholder="http://192.168.6.64:8088" required /></label>
        <label>用户名 <input name="user" required /></label>
        <label>密码 <input name="password" type="password" required /></label>
        <label>刷新间隔 <input name="refresh" value="30s" /></label>
        <div class="actions">
          <button type="submit">启动</button>
          <button type="button" class="danger" id="stop">停止</button>
          <button type="button" class="secondary" id="reload">刷新</button>
          <span class="status" id="status">未运行</span>
        </div>
      </form>
    </section>
    <section class="panel">
      <pre id="logs">等待启动...</pre>
    </section>
  </main>
  <script>
    const form = document.querySelector("#form");
    const statusEl = document.querySelector("#status");
    const logsEl = document.querySelector("#logs");
    const api = async (path, options = {}) => {
      const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...options });
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
      return res.json();
    };
    const render = (data) => {
      form.controlUrl.value = data.config?.controlUrl || "";
      form.user.value = data.config?.user || "";
      form.refresh.value = data.config?.refresh || "30s";
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
