//go:build windows

package main

import (
	_ "embed"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed tunnel-client-gui.exe
var guiBinaryBytes []byte

func openDashboardWindow(url string) {
	// 1. Get current executable directory
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get current executable path: %v", err)
		return
	}
	dir := filepath.Dir(exe)
	guiPath := filepath.Join(dir, "tunnel-client-gui.exe")

	// 2. Check if tunnel-client-gui.exe exists. If not, extract it from embedded bytes.
	if _, err := os.Stat(guiPath); err != nil {
		log.Printf("tunnel-client-gui.exe not found at %s, extracting embedded GUI binary...", guiPath)
		if len(guiBinaryBytes) > 0 {
			err = os.WriteFile(guiPath, guiBinaryBytes, 0755)
			if err != nil {
				log.Printf("Failed to extract embedded GUI binary: %v", err)
				guiPath = "tunnel-client-gui.exe" // fall back to path search
			}
		} else {
			log.Println("Embedded GUI binary is empty")
			guiPath = "tunnel-client-gui.exe" // fall back to path search
		}
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
