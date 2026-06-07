//go:build windows

package main

import (
	"fmt"
	"time"
)

func runDesktopLauncher(_ string, _ string, _ time.Duration) error {
	return fmt.Errorf("Windows GUI is provided by TunnelClient.exe; run tunnel-client-core.exe with -no-gui for command line mode")
}
