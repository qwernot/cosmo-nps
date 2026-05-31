package engine

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	FRP = "frp"
	NPS = "nps"
)

type Config struct {
	FRPSBin    string `json:"frpsBin"`
	FRPSConfig string `json:"frpsConfig"`
	FRPSPort   int    `json:"frpsPort"`
	NPSBin     string `json:"npsBin"`
	NPSWorkDir string `json:"npsWorkDir"`
	NPSPort    int    `json:"npsPort"`
	Embedded   bool   `json:"embedded"`
}

type Status struct {
	Engine     string `json:"engine"`
	Configured bool   `json:"configured"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	Port       int    `json:"port,omitempty"`
	PortOpen   bool   `json:"portOpen"`
	Binary     string `json:"binary,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
	WorkDir    string `json:"workDir,omitempty"`
	Message    string `json:"message,omitempty"`
}

type Manager struct {
	cfg   Config
	mu    sync.Mutex
	procs map[string]*exec.Cmd
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:   cfg,
		procs: map[string]*exec.Cmd{},
	}
}

func (m *Manager) Config() Config {
	return m.cfg
}

func (m *Manager) Statuses() []Status {
	return []Status{m.Status(FRP), m.Status(NPS)}
}

func (m *Manager) Status(name string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked(name)
}

func (m *Manager) Start(name string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.statusLocked(name); st.Running {
		return st, nil
	}
	cmd, err := m.command(name)
	if err != nil {
		return m.statusLocked(name), err
	}
	if err := cmd.Start(); err != nil {
		return m.statusLocked(name), err
	}
	m.procs[name] = cmd
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if m.procs[name] == cmd {
			delete(m.procs, name)
		}
		m.mu.Unlock()
	}()
	time.Sleep(250 * time.Millisecond)
	return m.statusLocked(name), nil
}

func (m *Manager) Stop(name string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd := m.procs[name]
	if cmd == nil || cmd.Process == nil {
		return m.statusLocked(name), nil
	}
	err := cmd.Process.Kill()
	delete(m.procs, name)
	time.Sleep(150 * time.Millisecond)
	return m.statusLocked(name), err
}

func (m *Manager) statusLocked(name string) Status {
	st := m.baseStatus(name)
	cmd := m.procs[name]
	if cmd != nil && cmd.Process != nil {
		st.Running = true
		st.PID = cmd.Process.Pid
	}
	if st.Port > 0 {
		st.PortOpen = portOpen(st.Port)
		if st.PortOpen {
			st.Running = true
			if st.PID == 0 {
				st.Message = "running; process is managed outside this control panel"
			}
		}
	}
	return st
}

func (m *Manager) baseStatus(name string) Status {
	switch name {
	case FRP:
		st := Status{
			Engine:     FRP,
			Configured: m.cfg.Embedded || m.cfg.FRPSBin != "",
			Port:       m.cfg.FRPSPort,
			Binary:     m.cfg.FRPSBin,
			ConfigPath: m.cfg.FRPSConfig,
		}
		if m.cfg.Embedded {
			st.Message = "embedded in tunnel-control"
		} else if !st.Configured {
			st.Message = "frps binary path is not configured"
		}
		return st
	case NPS:
		st := Status{
			Engine:     NPS,
			Configured: m.cfg.Embedded || m.cfg.NPSBin != "",
			Port:       m.cfg.NPSPort,
			Binary:     m.cfg.NPSBin,
			WorkDir:    m.cfg.NPSWorkDir,
		}
		if m.cfg.Embedded {
			st.Message = "embedded in tunnel-control"
		} else if !st.Configured {
			st.Message = "nps binary path is not configured"
		}
		return st
	default:
		return Status{Engine: name, Message: "unknown engine"}
	}
}

func (m *Manager) command(name string) (*exec.Cmd, error) {
	if m.cfg.Embedded {
		return nil, fmt.Errorf("%s is embedded in tunnel-control; restart the container to restart it", name)
	}
	switch name {
	case FRP:
		if m.cfg.FRPSBin == "" {
			return nil, fmt.Errorf("frps binary path is not configured")
		}
		args := []string{}
		if m.cfg.FRPSConfig != "" {
			args = append(args, "-c", m.cfg.FRPSConfig)
		}
		cmd := exec.Command(m.cfg.FRPSBin, args...)
		cmd.Dir = workDir(m.cfg.FRPSBin, "")
		return cmd, nil
	case NPS:
		if m.cfg.NPSBin == "" {
			return nil, fmt.Errorf("nps binary path is not configured")
		}
		cmd := exec.Command(m.cfg.NPSBin)
		cmd.Dir = workDir(m.cfg.NPSBin, m.cfg.NPSWorkDir)
		return cmd, nil
	default:
		return nil, fmt.Errorf("unknown engine %q", name)
	}
}

func workDir(bin, configured string) string {
	if configured != "" {
		return configured
	}
	if bin == "" {
		return ""
	}
	if runtime.GOOS == "windows" && filepath.VolumeName(bin) == "" && !filepath.IsAbs(bin) {
		return ""
	}
	return filepath.Dir(bin)
}

func portOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
