//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "TunnelClient"
const serviceDesc = "NPS Multi-Node Tunnel Client Service"

type tunnelService struct{}

func (m *tunnelService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// Load configuration
	cfg, err := loadServiceConfig()
	if err != nil {
		log.Printf("Service config load failed: %v", err)
		changes <- svc.Status{State: svc.Stopped}
		return
	}

	refreshDur, err := time.ParseDuration(cfg.Refresh)
	if err != nil {
		refreshDur = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := runClientCtx(ctx, cfg.ControlURL, cfg.User, cfg.Password, refreshDur)
		if err != nil {
			log.Printf("Service client thread exited: %v", err)
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Println("Service stop request received")
				cancel()
				break loop
			default:
				log.Printf("Unexpected control request: %d", c.Cmd)
			}
		}
	}
	changes <- svc.Status{State: svc.StopPending}
	return
}

func runAsService() bool {
	// 1. Check if running under SCM
	inService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	if inService {
		go func() {
			err := svc.Run(serviceName, &tunnelService{})
			if err != nil {
				log.Printf("Service execution failed: %v", err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
		return true
	}

	// 2. Check service CLI commands (e.g. -service-install, -service-uninstall)
	for _, arg := range os.Args[1:] {
		if arg == "-service-install" {
			err := installService(serviceName, serviceDesc)
			if err != nil {
				fmt.Printf("Failed to install service: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Service %s installed successfully.\n", serviceName)
			os.Exit(0)
		}
		if arg == "-service-uninstall" {
			err := uninstallService(serviceName)
			if err != nil {
				fmt.Printf("Failed to uninstall service: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Service %s uninstalled successfully.\n", serviceName)
			os.Exit(0)
		}
	}

	return false
}

func installService(name, desc string) error {
	exepath, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}
	s, err = m.CreateService(name, exepath, mgr.Config{
		DisplayName: "Tunnel Client Service",
		Description: desc,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	return nil
}

func uninstallService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %s is not installed", name)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		return err
	}
	return nil
}

func loadServiceConfig() (launcherConfig, error) {
	// Try loading from directory next to executable first (portable mode)
	exe, err := os.Executable()
	if err == nil {
		path := filepath.Join(filepath.Dir(exe), "client.json")
		if b, err := os.ReadFile(path); err == nil {
			var cfg launcherConfig
			if err := json.Unmarshal(b, &cfg); err == nil && cfg.ControlURL != "" {
				log.Printf("Loaded service config from portable path: %s", path)
				return cfg, nil
			}
		}
	}

	// Fallback to User Profile AppData
	path := launcherConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return launcherConfig{}, fmt.Errorf("config file not found at %s: %w", path, err)
	}
	var cfg launcherConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return launcherConfig{}, err
	}
	log.Printf("Loaded service config from AppData path: %s", path)
	return cfg, nil
}
