//go:build !windows

package main

func openDashboardWindow(url string) {
	go openBrowser(url)
}

func allowSetForegroundWindow() {}
