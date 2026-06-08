//go:build windows

package main

import (
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
)

const registryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const appRegistryName = "TunnelClient"

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
		return k.SetStringValue(appRegistryName, `"`+exePath+`" -silent`)
	} else {
		return k.DeleteValue(appRegistryName)
	}
}

func isAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, registryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	_, _, err = k.GetStringValue(appRegistryName)
	return err == nil
}

