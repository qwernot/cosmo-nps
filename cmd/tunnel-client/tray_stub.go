//go:build !(windows || (darwin && cgo) || (linux && cgo))

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func startDesktopTray(addr string, silent bool) {
	// Headless wait loop
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	closeAllClients()
}

func setTrayAutoStartChecked(checked bool) {
	// No-op
}
