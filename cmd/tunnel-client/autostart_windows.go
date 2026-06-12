//go:build windows

package main

import (
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const registryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const appRegistryName = "CosmoNPSClient"
const legacyAppRegistryName = "TunnelClient"

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	getConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	showWindow            = user32.NewProc("ShowWindow")
)

func hideConsoleWindow() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}

	var pids [2]uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	if count == 1 {
		// Only this process is using this console, safe to hide
		// SW_HIDE = 0
		showWindow.Call(hwnd, 0)
	}
}

func setAutoStart(enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enabled {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exePath, err := filepath.Abs(exe)
		if err != nil {
			exePath = exe
		}
		// Pass -silent flag on boot startup so it doesn't open browser
		if err := k.SetStringValue(appRegistryName, `"`+exePath+`" -silent`); err != nil {
			return err
		}
		_ = deleteRegistryValue(k, legacyAppRegistryName)
		return nil
	}
	if err := deleteRegistryValue(k, appRegistryName); err != nil {
		return err
	}
	return deleteRegistryValue(k, legacyAppRegistryName)
}

func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	for _, name := range []string{appRegistryName, legacyAppRegistryName} {
		if _, _, err = k.GetStringValue(name); err == nil {
			return true
		}
	}
	return false
}

func deleteRegistryValue(k registry.Key, name string) error {
	err := k.DeleteValue(name)
	if err == nil || err == registry.ErrNotExist {
		return nil
	}
	return err
}
