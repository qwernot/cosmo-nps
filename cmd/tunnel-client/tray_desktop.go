//go:build windows || (darwin && cgo) || (linux && cgo)

package main

import (
	"log"
	"os"

	"github.com/getlantern/systray"
)

var mAutoStart *systray.MenuItem

func startDesktopTray(addr string, silent bool) {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTooltip("Cosmo NPS - NPS Client")
	systray.SetIcon(generateIconBytes())

	mOpen := systray.AddMenuItem("Open Cosmo NPS", "Open the Cosmo NPS client dashboard")
	mAutoStart = systray.AddMenuItemCheckbox("Start with Windows", "Run Cosmo NPS silently after Windows login", isAutoStartEnabled())
	systray.AddSeparator()
	mExit := systray.AddMenuItem("Exit", "Close Cosmo NPS")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openDashboardWindow("http://" + globalAddr)
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
	closeDashboardWindow()
	os.Exit(0)
}

func setTrayAutoStartChecked(checked bool) {
	if mAutoStart != nil {
		if checked {
			mAutoStart.Check()
		} else {
			mAutoStart.Uncheck()
		}
	}
}
