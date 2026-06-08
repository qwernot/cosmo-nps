package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/getlantern/systray"
	"qwernot/tunnel-control/internal/core"
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
	running bool
	started time.Time
	logs    []string
	tunnels []core.Tunnel
	cancel  context.CancelFunc
}

var globalState = &launcherState{
	logs: make([]string, 0),
}

var (
	globalAddr string
	mAutoStart *systray.MenuItem
)

func (s *launcherState) AppendLog(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLocked(line)
}

func (s *launcherState) appendLocked(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	s.logs = append(s.logs, time.Now().Format("15:04:05")+" "+line)
	if len(s.logs) > 400 {
		s.logs = s.logs[len(s.logs)-400:]
	}
}

func (s *launcherState) UpdateTunnels(tunnels []core.Tunnel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnels = tunnels
}

// BeegoUILogger redirects ehang.io/nps (beego logs) to launcher UI logs
type BeegoUILogger struct{}

func (b *BeegoUILogger) Init(config string) error { return nil }
func (b *BeegoUILogger) WriteMsg(when time.Time, msg string, level int) error {
	levelStr := "INFO"
	switch level {
	case logs.LevelDebug:
		levelStr = "DEBUG"
	case logs.LevelInfo:
		levelStr = "INFO"
	case logs.LevelWarning:
		levelStr = "WARN"
	case logs.LevelError:
		levelStr = "ERROR"
	}
	globalState.AppendLog(fmt.Sprintf("[%s] %s", levelStr, msg))
	return nil
}
func (b *BeegoUILogger) Destroy() {}
func (b *BeegoUILogger) Flush()   {}

// goLogWriter redirects standard go log package output to launcher UI logs
type goLogWriter struct{}

func (w *goLogWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	msg := string(p)
	msg = strings.TrimSuffix(msg, "\n")
	msg = strings.TrimSuffix(msg, "\r")

	if !strings.Contains(msg, "[INFO]") && !strings.Contains(msg, "[WARN]") && !strings.Contains(msg, "[ERROR]") {
		msg = "[INFO] " + msg
	}
	globalState.AppendLog(msg)
	return len(p), nil
}

func init() {
	logs.Register("client_ui_log", func() logs.Logger {
		return &BeegoUILogger{}
	})
}

func runLauncher(addr, controlURL string, refresh time.Duration, silent bool) error {
	globalAddr = addr
	cfg := loadLauncherConfig()
	if cfg.ControlURL == "" {
		cfg.ControlURL = controlURL
	}
	if cfg.Refresh == "" {
		cfg.Refresh = refresh.String()
	}

	globalState.mu.Lock()
	globalState.cfg = cfg
	globalState.mu.Unlock()

	// Redirect standard logger and beego logger to UI logs
	log.SetOutput(&goLogWriter{})
	logs.SetLogger("client_ui_log", "")

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(launcherHTML))
	})
	mux.HandleFunc("GET /api/status", globalState.status)
	mux.HandleFunc("POST /api/start", globalState.start)
	mux.HandleFunc("POST /api/stop", globalState.stop)
	mux.HandleFunc("GET /api/settings", globalState.getSettings)
	mux.HandleFunc("POST /api/settings", globalState.setSettings)

	// Start HTTP server in background thread
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("HTTP server stopped: %v", err)
		}
	}()

	// Auto-connect on startup if config is complete
	if cfg.ControlURL != "" && cfg.User != "" && cfg.Password != "" {
		log.Printf("Saved config found. Auto-connecting in background...")
		refreshDur, err := time.ParseDuration(cfg.Refresh)
		if err != nil {
			refreshDur = 30 * time.Second
		}
		globalState.mu.Lock()
		ctx, cancel := context.WithCancel(context.Background())
		globalState.cancel = cancel
		globalState.running = true
		globalState.started = time.Now()
		globalState.mu.Unlock()

		go func() {
			err := runClientCtx(ctx, cfg.ControlURL, cfg.User, cfg.Password, refreshDur)
			globalState.mu.Lock()
			globalState.running = false
			globalState.cancel = nil
			globalState.tunnels = nil
			if err != nil {
				globalState.appendLocked("[ERROR] launcher: tunnel-client exited with error: " + err.Error())
			} else {
				globalState.appendLocked("[INFO] launcher: tunnel-client stopped")
			}
			globalState.mu.Unlock()
		}()
	}

	// Open browser if not in silent mode
	if !silent {
		go openBrowser("http://" + addr)
	}

	// Start system tray (blocks on main thread)
	systray.Run(onReady, onExit)
	return nil
}

func onReady() {
	systray.SetTooltip("Tunnel Client - NPS Multi-Node Tunnel")
	systray.SetIcon(generateIconBytes())

	mOpen := systray.AddMenuItem("打开主面板 (Open Dashboard)", "打开客户端网页管理主面板")
	mAutoStart = systray.AddMenuItemCheckbox("开机自启动 (Boot Startup)", "设置开机自启动", isAutoStartEnabled())
	systray.AddSeparator()
	mExit := systray.AddMenuItem("退出 (Exit)", "关闭并退出客户端")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser("http://" + globalAddr)
			case <-mAutoStart.ClickedCh:
				enabled := !isAutoStartEnabled()
				err := setAutoStart(enabled)
				if err != nil {
					log.Printf("Failed to toggle auto start: %v", err)
				} else {
					if enabled {
						mAutoStart.Check()
					} else {
						mAutoStart.Uncheck()
					}
				}
			case <-mExit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}

func onExit() {
	closeAllClients()
	os.Exit(0)
}

//go:embed app.ico
var appIconBytes []byte

func generateIconBytes() []byte {
	if len(appIconBytes) > 0 {
		return appIconBytes
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{13, 18, 38, 255}}, image.Point{}, draw.Src)
	cyan := color.RGBA{34, 211, 238, 255}
	blue := color.RGBA{59, 130, 246, 255}
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			dx := float64(x - 32)
			dy := float64(y - 32)
			dist := dx*dx + dy*dy
			if dist < 225 {
				img.Set(x, y, cyan)
			} else if dist < 400 {
				img.Set(x, y, blue)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func (s *launcherState) status(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hiddenCfg := s.cfg
	hiddenCfg.Password = ""
	writeLauncherJSON(w, map[string]any{
		"config":  hiddenCfg,
		"running": s.running,
		"started": s.started,
		"tunnels": s.tunnels,
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
	refreshDur, err := time.ParseDuration(cfg.Refresh)
	if err != nil {
		writeLauncherError(w, http.StatusBadRequest, fmt.Errorf("invalid refresh interval: %w", err))
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeLauncherError(w, http.StatusConflict, fmt.Errorf("client is already running"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.cfg = cfg
	s.running = true
	s.started = time.Now()
	s.tunnels = nil
	s.logs = make([]string, 0)
	s.appendLocked("[INFO] launcher: starting tunnel-client...")
	s.mu.Unlock()

	_ = saveLauncherConfig(cfg)

	go func() {
		err := runClientCtx(ctx, cfg.ControlURL, cfg.User, cfg.Password, refreshDur)
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.tunnels = nil
		if err != nil {
			s.appendLocked("[ERROR] launcher: tunnel-client exited with error: " + err.Error())
		} else {
			s.appendLocked("[INFO] launcher: tunnel-client stopped")
		}
		s.mu.Unlock()
	}()

	writeLauncherJSON(w, map[string]any{"status": "started"})
}

func (s *launcherState) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cancel := s.cancel
	if !s.running || cancel == nil {
		s.mu.Unlock()
		writeLauncherJSON(w, map[string]any{"status": "stopped"})
		return
	}
	s.appendLocked("[INFO] launcher: stopping tunnel-client...")
	s.mu.Unlock()

	cancel()
	writeLauncherJSON(w, map[string]any{"status": "stopping"})
}

func (s *launcherState) getSettings(w http.ResponseWriter, r *http.Request) {
	writeLauncherJSON(w, map[string]any{
		"autoStart": isAutoStartEnabled(),
	})
}

func (s *launcherState) setSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AutoStart bool `json:"autoStart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLauncherError(w, http.StatusBadRequest, err)
		return
	}
	err := setAutoStart(req.AutoStart)
	if err != nil {
		writeLauncherError(w, http.StatusInternalServerError, err)
		return
	}
	if mAutoStart != nil {
		if req.AutoStart {
			mAutoStart.Check()
		} else {
			mAutoStart.Uncheck()
		}
	}
	writeLauncherJSON(w, map[string]any{"status": "ok"})
}

func loadLauncherConfig() launcherConfig {
	var cfg launcherConfig
	b, err := os.ReadFile(launcherConfigPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}

func saveLauncherConfig(cfg launcherConfig) error {
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
			cmd = exec.Command(browser, "--app="+url, "--window-size=1080,780")
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

// Global update method to link main.go's sync loop to globalState
func (s *launcherState) updateTunnels(nodes []bootstrapNode) {
	var list []core.Tunnel
	for _, node := range nodes {
		for _, t := range node.Tunnels {
			list = append(list, t)
		}
	}
	s.UpdateTunnels(list)
}

const launcherHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Tunnel Client Dashboard</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500&family=Outfit:wght@300;400;500;600;700&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg-primary: #080a14;
      --bg-secondary: #0e132b;
      --accent-cyan: #3b82f6;
      --accent-blue: #1d4ed8;
      --accent-glow: rgba(59, 130, 246, 0.15);
      --text-main: #f8fafc;
      --text-muted: #94a3b8;
      --border-color: rgba(255, 255, 255, 0.06);
      --emerald: #10b981;
      --rose: #f43f5e;
      --amber: #f59e0b;
    }
    
    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
    }
    
    body {
      font-family: 'Outfit', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      color: var(--text-main);
      background-color: var(--bg-primary);
      min-height: 100vh;
      overflow-x: hidden;
      background-image: 
        radial-gradient(circle at 10% 20%, rgba(34, 211, 238, 0.08) 0%, transparent 40%),
        radial-gradient(circle at 90% 80%, rgba(59, 130, 246, 0.08) 0%, transparent 40%);
    }

    main {
      max-width: 1200px;
      margin: 0 auto;
      padding: 24px;
      display: grid;
      grid-template-columns: 320px 1fr;
      gap: 24px;
    }

    /* Glassmorphism Panel styles */
    .glass-panel {
      background: rgba(13, 18, 38, 0.6);
      backdrop-filter: blur(16px);
      -webkit-backdrop-filter: blur(16px);
      border: 1px solid var(--border-color);
      border-radius: 20px;
      box-shadow: 0 10px 30px rgba(0, 0, 0, 0.3);
      padding: 24px;
    }

    /* Sidebar Layout */
    .sidebar {
      display: flex;
      flex-direction: column;
      gap: 24px;
      height: calc(100vh - 48px);
      position: sticky;
      top: 24px;
      background: rgba(11, 15, 25, 0.85) !important;
      box-shadow: 0 10px 40px rgba(0, 0, 0, 0.45);
    }

    .brand {
      display: flex;
      align-items: center;
      gap: 16px;
    }

    .brand-logo {
      width: 48px;
      height: 48px;
      background: linear-gradient(135deg, var(--accent-cyan), var(--accent-blue));
      border-radius: 14px;
      display: grid;
      place-items: center;
      box-shadow: 0 0 20px rgba(34, 211, 238, 0.4);
    }

    .brand-logo svg {
      width: 24px;
      height: 24px;
      fill: #fff;
    }

    .brand-title h1 {
      font-size: 20px;
      font-weight: 700;
      letter-spacing: 0.5px;
      background: linear-gradient(to right, #ffffff, #cbd5e1);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
    }

    .brand-title p {
      font-size: 12px;
      color: var(--text-muted);
      font-weight: 400;
    }

    /* Navigation Menu */
    .nav-menu {
      display: flex;
      flex-direction: column;
      gap: 10px;
      margin-top: 8px;
    }

    .nav-item {
      display: flex;
      align-items: center;
      gap: 14px;
      padding: 14px 20px;
      border-radius: 12px;
      cursor: pointer;
      color: var(--text-muted);
      font-weight: 500;
      font-size: 16px;
      transition: all 0.2s ease-in-out;
      user-select: none;
    }

    .nav-item:hover {
      color: var(--text-main);
      background: rgba(255, 255, 255, 0.04);
    }

    .nav-item.active {
      color: #fff;
      background: #2563eb; /* Solid bright blue matching user's screenshot */
      box-shadow: 0 4px 15px rgba(37, 99, 235, 0.3);
    }

    .nav-icon {
      font-size: 18px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 20px;
      height: 20px;
    }

    /* Status Card */
    .status-card {
      background: rgba(0, 0, 0, 0.2);
      border-radius: 16px;
      padding: 20px;
      border: 1px solid rgba(255, 255, 255, 0.04);
      display: flex;
      flex-direction: column;
      gap: 16px;
      margin-top: auto; /* Push to bottom of sidebar */
    }

    .status-badge {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      align-self: flex-start;
      padding: 6px 14px;
      border-radius: 99px;
      font-size: 13px;
      font-weight: 600;
      background: rgba(156, 163, 175, 0.1);
      color: var(--text-muted);
      transition: all 0.3s ease;
    }

    .status-badge.online {
      background: rgba(16, 185, 129, 0.1);
      color: var(--emerald);
      box-shadow: 0 0 15px rgba(16, 185, 129, 0.1);
    }

    .status-badge.connecting {
      background: rgba(245, 158, 11, 0.1);
      color: var(--amber);
      box-shadow: 0 0 15px rgba(245, 158, 11, 0.1);
    }

    .status-badge::before {
      content: "";
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: currentColor;
    }

    .status-badge.online::before, .status-badge.connecting::before {
      animation: pulse 1.8s infinite;
    }

    @keyframes pulse {
      0% { transform: scale(0.95); opacity: 0.8; }
      50% { transform: scale(1.25); opacity: 1; box-shadow: 0 0 10px currentColor; }
      100% { transform: scale(0.95); opacity: 0.8; }
    }

    .meta-list {
      display: flex;
      flex-direction: column;
      gap: 12px;
    }

    .meta-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      font-size: 13px;
    }

    .meta-item span {
      color: var(--text-muted);
    }

    .meta-item strong {
      color: var(--text-main);
      max-width: 160px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    /* Content Area & Tabs */
    .content-area {
      display: flex;
      flex-direction: column;
      position: relative;
    }

    .tab-content {
      display: none;
      flex-direction: column;
      gap: 24px;
    }

    .tab-content.active {
      display: flex;
    }

    h2 {
      font-size: 18px;
      font-weight: 600;
      margin-bottom: 16px;
      display: flex;
      align-items: center;
      gap: 10px;
    }

    h2 svg {
      width: 18px;
      height: 18px;
      fill: var(--accent-cyan);
    }

    /* Connection Card Form */
    form {
      display: grid;
      grid-template-columns: repeat(2, 1fr);
      gap: 16px;
    }

    .form-group {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }

    .form-group.full-width {
      grid-column: 1 / -1;
    }

    label {
      font-size: 13px;
      font-weight: 500;
      color: var(--text-muted);
    }

    input[type="text"], input[type="url"], input[type="password"] {
      background: rgba(0, 0, 0, 0.3);
      border: 1px solid var(--border-color);
      border-radius: 12px;
      padding: 12px 16px;
      color: #fff;
      font-family: inherit;
      font-size: 14px;
      outline: none;
      transition: all 0.3s ease;
    }

    input[type="text"]:focus, input[type="url"]:focus, input[type="password"]:focus {
      border-color: var(--accent-cyan);
      box-shadow: 0 0 0 4px var(--accent-glow);
    }

    .form-actions {
      grid-column: 1 / -1;
      display: flex;
      gap: 12px;
      margin-top: 8px;
    }

    button {
      flex: 1;
      border: none;
      border-radius: 12px;
      padding: 12px 24px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.3s ease;
      font-family: inherit;
    }

    button[type="submit"] {
      background: linear-gradient(135deg, var(--accent-cyan), var(--accent-blue));
      color: #fff;
      box-shadow: 0 4px 15px rgba(34, 211, 238, 0.2);
    }

    button[type="submit"]:hover {
      box-shadow: 0 4px 20px rgba(34, 211, 238, 0.35);
      transform: translateY(-1px);
    }

    button.danger-btn {
      background: rgba(244, 63, 94, 0.1);
      color: var(--rose);
      border: 1px solid rgba(244, 63, 94, 0.2);
    }

    button.danger-btn:hover {
      background: rgba(244, 63, 94, 0.18);
      transform: translateY(-1px);
    }

    /* Active Tunnels Section */
    .tunnel-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
      gap: 16px;
      margin-top: 8px;
    }

    .tunnel-card {
      background: rgba(255, 255, 255, 0.02);
      border: 1px solid var(--border-color);
      border-radius: 14px;
      padding: 16px;
      display: flex;
      flex-direction: column;
      gap: 12px;
      transition: all 0.3s ease;
    }

    .tunnel-card:hover {
      background: rgba(255, 255, 255, 0.04);
      border-color: rgba(34, 211, 238, 0.3);
      transform: translateY(-2px);
    }

    .tunnel-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
    }

    .tunnel-mode {
      font-size: 11px;
      font-weight: 700;
      padding: 3px 8px;
      border-radius: 6px;
      text-transform: uppercase;
    }

    .tunnel-mode.tcp { background: rgba(59, 130, 246, 0.15); color: #60a5fa; }
    .tunnel-mode.udp { background: rgba(168, 85, 247, 0.15); color: #c084fc; }
    .tunnel-mode.socks5 { background: rgba(234, 179, 8, 0.15); color: #facc15; }
    .tunnel-mode.http { background: rgba(20, 184, 166, 0.15); color: #2dd4bf; }
    .tunnel-mode.https { background: rgba(236, 72, 153, 0.15); color: #f472b6; }

    .tunnel-status {
      font-size: 12px;
      color: var(--emerald);
      display: flex;
      align-items: center;
      gap: 4px;
    }

    .tunnel-status::before {
      content: "";
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: currentColor;
    }

    .tunnel-addrs {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }

    .addr-row {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 13px;
    }

    .addr-label {
      color: var(--text-muted);
      width: 42px;
      font-size: 11px;
      text-transform: uppercase;
    }

    .addr-val {
      font-family: 'JetBrains Mono', monospace;
      color: var(--text-main);
    }

    .tunnel-remark {
      font-size: 12px;
      color: var(--text-muted);
      border-top: 1px solid rgba(255, 255, 255, 0.04);
      padding-top: 8px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .empty-state {
      grid-column: 1 / -1;
      text-align: center;
      padding: 40px;
      color: var(--text-muted);
      font-size: 14px;
      background: rgba(255, 255, 255, 0.01);
      border-radius: 14px;
      border: 1px dashed rgba(255, 255, 255, 0.05);
    }

    /* Logs Section */
    .logs-panel {
      display: flex;
      flex-direction: column;
    }

    .logs-container {
      background: #02040a;
      border: 1px solid var(--border-color);
      border-radius: 12px;
      padding: 16px;
      height: 480px;
      overflow-y: auto;
      font-family: 'JetBrains Mono', monospace;
      font-size: 12px;
      line-height: 1.6;
      white-space: pre-wrap;
    }

    .log-line {
      display: block;
      margin-bottom: 4px;
    }

    .log-time {
      color: #57606a;
      margin-right: 8px;
    }

    .log-level {
      font-weight: 500;
      margin-right: 6px;
    }

    .log-level.info { color: #58a6ff; }
    .log-level.warn { color: #d29922; }
    .log-level.error { color: #f85149; }
    .log-level.trace { color: #8b949e; }
    .log-level.debug { color: #c9d1d9; }

    .log-text {
      color: #c9d1d9;
    }

    /* Settings Switch Layout */
    .setting-item {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 16px 20px;
      border-radius: 14px;
      background: rgba(255, 255, 255, 0.01);
      border: 1px solid var(--border-color);
    }

    .setting-info {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }

    .setting-title {
      font-size: 15px;
      font-weight: 600;
    }

    .setting-desc {
      font-size: 12px;
      color: var(--text-muted);
    }

    /* Custom Switch Toggle */
    .switch {
      position: relative;
      display: inline-block;
      width: 48px;
      height: 26px;
    }

    .switch input { 
      opacity: 0;
      width: 0;
      height: 0;
    }

    .slider {
      position: absolute;
      cursor: pointer;
      inset: 0;
      background-color: rgba(255, 255, 255, 0.1);
      transition: .4s;
      border-radius: 34px;
      border: 1px solid var(--border-color);
    }

    .slider::before {
      position: absolute;
      content: "";
      height: 18px;
      width: 18px;
      left: 3px;
      bottom: 3px;
      background-color: white;
      transition: .4s;
      border-radius: 50%;
    }

    input:checked + .slider {
      background-color: var(--accent-cyan);
      box-shadow: 0 0 10px var(--accent-glow);
    }

    input:focus + .slider {
      box-shadow: 0 0 1px var(--accent-cyan);
    }

    input:checked + .slider::before {
      transform: translateX(22px);
    }

    /* Custom Scrollbars */
    ::-webkit-scrollbar {
      width: 6px;
      height: 6px;
    }

    ::-webkit-scrollbar-track {
      background: transparent;
    }

    ::-webkit-scrollbar-thumb {
      background: rgba(255, 255, 255, 0.1);
      border-radius: 4px;
    }

    ::-webkit-scrollbar-thumb:hover {
      background: rgba(255, 255, 255, 0.2);
    }

    @media (max-width: 900px) {
      main {
        grid-template-columns: 1fr;
      }
      .sidebar {
        height: auto;
        position: static;
      }
    }
  </style>
</head>
<body>
  <main>
    <!-- Sidebar -->
    <aside class="sidebar glass-panel">
      <div class="brand">
        <div class="brand-logo">
          <svg viewBox="0 0 24 24">
            <path d="M17.657 16.657L13.414 20.9a1.998 1.998 0 0 1-2.827 0l-4.244-4.243a8 8 0 1 1 11.314 0zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"/>
          </svg>
        </div>
        <div class="brand-title">
          <h1>Tunnel Client</h1>
          <p>NPS 多节点隧道客户端</p>
        </div>
      </div>

      <!-- Navigation Menu -->
      <nav class="nav-menu">
        <div class="nav-item active" data-tab="client">
          <span class="nav-icon">🔗</span>
          <span>客户端</span>
        </div>
        <div class="nav-item" data-tab="logs">
          <span class="nav-icon">📋</span>
          <span>连接日志</span>
        </div>
        <div class="nav-item" data-tab="settings">
          <span class="nav-icon">⚙️</span>
          <span>设置</span>
        </div>
      </nav>

      <div class="status-card">
        <span class="status-badge" id="status-badge">未连接</span>
        <div class="meta-list">
          <div class="meta-item">
            <span>总控服务</span>
            <strong id="meta-server">-</strong>
          </div>
          <div class="meta-item">
            <span>连接账号</span>
            <strong id="meta-user">-</strong>
          </div>
          <div class="meta-item">
            <span>活动隧道</span>
            <strong id="meta-tunnels">0 个</strong>
          </div>
          <div class="meta-item">
            <span>运行时间</span>
            <strong id="meta-uptime">-</strong>
          </div>
        </div>
      </div>
    </aside>

    <!-- Content Area -->
    <section class="content-area">
      <!-- CLIENT TAB -->
      <div class="tab-content active" id="tab-client">
        <!-- Config Panel -->
        <section class="glass-panel">
          <h2>
            <svg viewBox="0 0 24 24"><path d="M19.43 12.98c.04-.32.07-.64.07-.98s-.03-.66-.07-.98l2.11-1.65c.19-.15.24-.42.12-.64l-2-3.46c-.12-.22-.39-.3-.61-.22l-2.49 1c-.52-.4-1.08-.73-1.69-.98l-.38-2.65C14.46 2.18 14.25 2 14 2h-4c-.25 0-.46.18-.49.42l-.38 2.65c-.61.25-1.17.59-1.69.98l-2.49-1c-.23-.09-.49 0-.61.22l-2 3.46c-.13.22-.07.49.12.64l2.11 1.65c-.04.32-.07.65-.07.98s.03.66.07.98l-2.11 1.65c-.19.15-.24.42-.12.64l2 3.46c.12.22.39.3.61.22l2.49-1c.52.4 1.08.73 1.69.98l.38 2.65c.03.24.24.42.49.42h4c.25 0 .46-.18.49-.42l.38-2.65c.61-.25 1.17-.59 1.69-.98l2.49 1c.23.09.49 0 .61-.22l2-3.46c.12-.22.07-.49-.12-.64l-2.11-1.65zM12 15.5c-1.93 0-3.5-1.57-3.5-3.5s1.57-3.5 3.5-3.5 3.5 1.57 3.5 3.5-1.57 3.5-3.5 3.5z"/></svg>
            连接设置
          </h2>
          <form id="connect-form">
            <div class="form-group full-width">
              <label>总控面板地址</label>
              <input name="controlUrl" type="url" placeholder="http://192.168.6.64:8088" required />
            </div>
            <div class="form-group">
              <label>用户名</label>
              <input name="user" type="text" placeholder="用户名" required />
            </div>
            <div class="form-group">
              <label>密码</label>
              <input name="password" type="password" placeholder="密码" required />
            </div>
            <div class="form-group">
              <label>定时重连间隔</label>
              <input name="refresh" type="text" value="30s" />
            </div>
            <div class="form-actions">
              <button type="submit" id="btn-start">启动客户端</button>
              <button type="button" class="danger-btn" id="btn-stop">停止</button>
            </div>
          </form>
        </section>

        <!-- Active Tunnels Panel -->
        <section class="glass-panel">
          <h2>
            <svg viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/></svg>
            本地启用中的穿透隧道
          </h2>
          <div class="tunnel-grid" id="tunnels-list">
            <div class="empty-state">未连接总控服务</div>
          </div>
        </section>
      </div>

      <!-- LOGS TAB -->
      <div class="tab-content" id="tab-logs">
        <!-- Logs Panel -->
        <section class="glass-panel logs-panel">
          <h2>
            <svg viewBox="0 0 24 24"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>
            连接日志
          </h2>
          <div class="logs-container" id="logs-view">等待连接...</div>
        </section>
      </div>

      <!-- SETTINGS TAB -->
      <div class="tab-content" id="tab-settings">
        <!-- Settings Panel -->
        <section class="glass-panel">
          <h2>
            <svg viewBox="0 0 24 24"><path d="M19.43 12.98c.04-.32.07-.64.07-.98s-.03-.66-.07-.98l2.11-1.65c.19-.15.24-.42.12-.64l-2-3.46c-.12-.22-.39-.3-.61-.22l-2.49 1c-.52-.4-1.08-.73-1.69-.98l-.38-2.65C14.46 2.18 14.25 2 14 2h-4c-.25 0-.46.18-.49.42l-.38 2.65c-.61.25-1.17.59-1.69.98l-2.49-1c-.23-.09-.49 0-.61.22l-2 3.46c-.13.22-.07.49.12.64l2.11 1.65c-.04.32-.07.65-.07.98s.03.66.07.98l-2.11 1.65c-.19.15-.24.42-.12.64l2 3.46c.12.22.39.3.61.22l2.49-1c.52.4 1.08.73 1.69.98l.38 2.65c.03.24.24.42.49.42h4c.25 0 .46-.18.49-.42l.38-2.65c.61-.25 1.17-.59 1.69-.98l2.49 1c.23.09.49 0 .61-.22l2-3.46c.12-.22.07-.49-.12-.64l-2.11-1.65zM12 15.5c-1.93 0-3.5-1.57-3.5-3.5s1.57-3.5 3.5-3.5 3.5 1.57 3.5 3.5-1.57 3.5-3.5 3.5z"/></svg>
            系统设置
          </h2>
          <div class="setting-item">
            <div class="setting-info">
              <span class="setting-title">开机自启动</span>
              <span class="setting-desc">当 Windows 系统启动时，在后台静默运行客户端</span>
            </div>
            <label class="switch">
              <input type="checkbox" id="setting-autostart" />
              <span class="slider"></span>
            </label>
          </div>
        </section>
      </div>
    </section>
  </main>

  <script>
    const form = document.querySelector("#connect-form");
    const statusBadge = document.querySelector("#status-badge");
    const tunnelsList = document.querySelector("#tunnels-list");
    const logsView = document.querySelector("#logs-view");
    
    const metaServer = document.querySelector("#meta-server");
    const metaUser = document.querySelector("#meta-user");
    const metaTunnels = document.querySelector("#meta-tunnels");
    const metaUptime = document.querySelector("#meta-uptime");
    
    let uptimeInterval = null;
    let startedTime = null;

    // Tab switching logic
    const navItems = document.querySelectorAll(".nav-item");
    const tabContents = document.querySelectorAll(".tab-content");
    
    navItems.forEach(item => {
      item.addEventListener("click", () => {
        const tab = item.getAttribute("data-tab");
        
        navItems.forEach(i => i.classList.remove("active"));
        tabContents.forEach(c => c.classList.remove("active"));
        
        item.classList.add("active");
        document.querySelector("#tab-" + tab).classList.add("active");
        
        if (tab === "settings") {
          loadSettings();
        }
      });
    });

    const api = async (path, options = {}) => {
      const res = await fetch(path, { headers: { "Content-Type": "application/json" }, ...options });
      if (!res.ok) {
        const errorText = await res.json().catch(() => ({})).then(data => data.error || res.statusText);
        throw new Error(errorText);
      }
      return res.json();
    };

    const updateUptime = () => {
      if (!startedTime) {
        metaUptime.textContent = "-";
        return;
      }
      const diff = Math.floor((new Date() - startedTime) / 1000);
      const hrs = Math.floor(diff / 3600);
      const mins = Math.floor((diff % 3600) / 60);
      const secs = diff % 60;
      metaUptime.textContent = pad(hrs) + ":" + pad(mins) + ":" + pad(secs);
    };

    const formatLogLine = (line) => {
      const match = line.match(/^(\d{2}:\d{2}:\d{2})\s(.*)$/);
      if (!match) return '<div class="log-line"><span class="log-text">' + escapeHtml(line) + '</span></div>';
      
      const time = match[1];
      let msg = match[2];
      let level = 'info';

      if (msg.includes("[ERROR]")) {
        level = 'error';
        msg = msg.replace("[ERROR] ", "");
      } else if (msg.includes("[WARN]")) {
        level = 'warn';
        msg = msg.replace("[WARN] ", "");
      } else if (msg.includes("[TRACE]")) {
        level = 'trace';
        msg = msg.replace("[TRACE] ", "");
      } else if (msg.includes("[DEBUG]")) {
        level = 'debug';
        msg = msg.replace("[DEBUG] ", "");
      } else if (msg.includes("[INFO]")) {
        level = 'info';
        msg = msg.replace("[INFO] ", "");
      }

      return '<div class="log-line"><span class="log-time">' + time + '</span><span class="log-level ' + level + '">[' + level.toUpperCase() + ']</span><span class="log-text">' + escapeHtml(msg) + '</span></div>';
    };

    const escapeHtml = (text) => {
      return text
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
    };

    const render = (data) => {
      // Inputs setup once
      if (document.activeElement.tagName !== 'INPUT') {
        if (data.config?.controlUrl) form.controlUrl.value = data.config.controlUrl;
        if (data.config?.user) form.user.value = data.config.user;
        if (data.config?.refresh) form.refresh.value = data.config.refresh;
      }

      // Sidebar meta update
      metaServer.textContent = data.config?.controlUrl || "-";
      metaUser.textContent = data.config?.user || "-";

      // Status styling
      if (data.running) {
        statusBadge.textContent = "运行中";
        statusBadge.className = "status-badge online";
        if (!uptimeInterval && data.started) {
          startedTime = new Date(data.started);
          uptimeInterval = setInterval(updateUptime, 1000);
          updateUptime();
        }
      } else {
        statusBadge.textContent = "已停止";
        statusBadge.className = "status-badge";
        clearInterval(uptimeInterval);
        uptimeInterval = null;
        startedTime = null;
        metaUptime.textContent = "-";
      }

      // Tunnels list rendering
      if (data.running) {
        if (data.tunnels && data.tunnels.length > 0) {
          metaTunnels.textContent = data.tunnels.length + " 个";
          tunnelsList.innerHTML = data.tunnels.map(t => {
            const isDomain = t.mode === 'http' || t.mode === 'https';
            const local = (t.localIp || '127.0.0.1') + ":" + t.localPort;
            const remote = isDomain ? (t.domains?.join(', ') || '-') : "端口 " + t.remotePort;
            return '<div class="tunnel-card">' +
                     '<div class="tunnel-header">' +
                       '<span class="tunnel-mode ' + t.mode + '">' + t.mode + '</span>' +
                       '<span class="tunnel-status">已启用</span>' +
                     '</div>' +
                     '<div class="tunnel-addrs">' +
                       '<div class="addr-row">' +
                         '<span class="addr-label">本地</span>' +
                         '<span class="addr-val">' + local + '</span>' +
                       '</div>' +
                       '<div class="addr-row">' +
                         '<span class="addr-label">穿透</span>' +
                         '<span class="addr-val" style="color:var(--accent-cyan)">' + remote + '</span>' +
                       '</div>' +
                     '</div>' +
                     '<div class="tunnel-remark">' + (t.remark || '未命名隧道') + '</div>' +
                   '</div>';
          }).join('');
        } else {
          metaTunnels.textContent = "0 个";
          tunnelsList.innerHTML = '<div class="empty-state">等待同步中，暂无活动隧道</div>';
        }
      } else {
        metaTunnels.textContent = "0 个";
        tunnelsList.innerHTML = '<div class="empty-state">客户端未启动，无活动隧道</div>';
      }

      // Console logs rendering
      const isScrolledToBottom = logsView.scrollHeight - logsView.clientHeight <= logsView.scrollTop + 20;
      logsView.innerHTML = (data.logs || []).map(formatLogLine).join("") || '<div class="log-line"><span class="log-text" style="color:var(--text-muted)">无控制台输出...</span></div>';
      if (isScrolledToBottom) {
        logsView.scrollTop = logsView.scrollHeight;
      }
    };

    const loadSettings = async () => {
      try {
        const data = await api("/api/settings");
        document.querySelector("#setting-autostart").checked = data.autoStart;
      } catch (err) {
        console.error("Failed to load settings:", err);
      }
    };

    document.querySelector("#setting-autostart").addEventListener("change", async (e) => {
      try {
        await api("/api/settings", {
          method: "POST",
          body: JSON.stringify({ autoStart: e.target.checked })
        });
      } catch (err) {
        alert("保存设置失败: " + err.message);
        e.target.checked = !e.target.checked;
      }
    });

    const refresh = async () => {
      try {
        const data = await api("/api/status");
        render(data);
      } catch (err) {
        logsView.innerHTML = '<div class="log-line"><span class="log-level error">[ERROR]</span><span class="log-text">' + escapeHtml(err.message) + '</span></div>';
      }
    };

    const pad = (num) => String(num).padStart(2, '0');

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const body = Object.fromEntries(new FormData(form).entries());
      try {
        await api("/api/start", { method: "POST", body: JSON.stringify(body) });
        refresh();
      } catch (err) {
        alert("启动失败: " + err.message);
      }
    });

    document.querySelector("#btn-stop").addEventListener("click", async () => {
      try {
        await api("/api/stop", { method: "POST" });
        refresh();
      } catch (err) {
        alert("停止失败: " + err.message);
      }
    });

    // Run poll
    refresh();
    setInterval(refresh, 1500);
  </script>
</body>
</html>`
