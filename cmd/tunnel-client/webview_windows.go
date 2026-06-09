//go:build windows

package main

import (
	_ "embed"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

//go:embed tunnel-client-gui.exe
var guiBinaryBytes []byte

func openDashboardWindow(url string) {
	// 1. Set extraction path to system temporary folder to keep the running directory clean
	tempDir := filepath.Join(os.TempDir(), "tunnel-client-gui")
	_ = os.MkdirAll(tempDir, 0755)
	guiPath := filepath.Join(tempDir, "tunnel-client-gui.exe")

	// 2. Determine if we need to overwrite/extract the embedded GUI (e.g. on first run or update)
	overwrite := false
	stat, err := os.Stat(guiPath)
	if err != nil {
		overwrite = true
	} else if stat.Size() != int64(len(guiBinaryBytes)) {
		log.Printf("GUI size mismatch: local=%d embedded=%d. Overwriting...", stat.Size(), len(guiBinaryBytes))
		overwrite = true
	}

	if overwrite {
		log.Printf("Extracting embedded GUI binary to %s...", guiPath)
		if len(guiBinaryBytes) > 0 {
			err = os.WriteFile(guiPath, guiBinaryBytes, 0755)
			if err != nil {
				log.Printf("Failed to extract embedded GUI binary: %v", err)
				guiPath = "tunnel-client-gui.exe" // fall back to local folder search
			}
		} else {
			log.Println("Embedded GUI binary is empty")
			guiPath = "tunnel-client-gui.exe"
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

func closeDashboardWindow() {
	log.Println("Closing active C# GUI windows...")
	cmd := exec.Command("taskkill", "/F", "/IM", "tunnel-client-gui.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}
