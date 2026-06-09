//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func openDashboardWindow(url string) {
	// 1. Get current executable directory
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get current executable path: %v", err)
		return
	}
	dir := filepath.Dir(exe)
	guiPath := filepath.Join(dir, "tunnel-client-gui.exe")

	// 2. Check if tunnel-client-gui.exe exists in the same folder
	if _, err := os.Stat(guiPath); err != nil {
		log.Printf("tunnel-client-gui.exe not found at %s, falling back to path search...", guiPath)
		guiPath = "tunnel-client-gui.exe"
	}

	// 3. Launch the C# GUI with the URL as an argument
	cmd := exec.Command(guiPath, url)
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to launch C# GUI (%s): %v. Falling back to default browser.", guiPath, err)
		go openBrowser(url)
	} else {
		log.Printf("Launched C# GUI: %s %s", guiPath, url)
	}
}

func allowSetForegroundWindow() {
	// C# GUI handles its own single-instance and foreground focus using nativeMutex,
	// so the Go side doesn't need to do any Win32 focus delegating.
}
