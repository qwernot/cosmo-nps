//go:build !windows

package main

func setAutoStart(enabled bool) error {
	return nil
}

func isAutoStartEnabled() bool {
	return false
}
