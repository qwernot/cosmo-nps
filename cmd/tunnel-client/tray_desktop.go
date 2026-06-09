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
	systray.SetTooltip("Tunnel Client - NPS Multi-Node Tunnel")
	systray.SetIcon(generateIconBytes())

	mOpen := systray.AddMenuItem("打开主面板 (Open Dashboard)", "打开客户端网页管理主面板")
	mAutoStart = systray.AddMenuItemCheckbox("开机自启动 (Boot Startup)", "设置开机自启动", isAutoStartEnabled())
	systray.AddSeparator()
	mExit := systray.AddMenuItem("退出 (Exit)", "关闭并退出客户端")

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
