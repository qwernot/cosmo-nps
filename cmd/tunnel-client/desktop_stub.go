//go:build !windows

package main

import "time"

func runDesktopLauncher(addr, controlURL string, refresh time.Duration, silent bool) error {
	return runLauncher(addr, controlURL, refresh, silent)
}
