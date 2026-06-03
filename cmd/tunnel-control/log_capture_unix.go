//go:build unix

package main

import (
	"io"
	"log"
	"os"
	"syscall"
)

func setupLogging(buffer *logBuffer) {
	if err := captureProcessOutput(buffer); err != nil {
		log.SetOutput(io.MultiWriter(os.Stderr, buffer))
		buffer.addLine("app", "log capture failed: "+err.Error())
	}
}

func captureProcessOutput(buffer *logBuffer) error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	originalStdout, err := syscall.Dup(syscall.Stdout)
	if err != nil {
		return err
	}
	original := os.NewFile(uintptr(originalStdout), "original-stdout")
	if err := syscall.Dup2(int(w.Fd()), syscall.Stdout); err != nil {
		return err
	}
	if err := syscall.Dup2(int(w.Fd()), syscall.Stderr); err != nil {
		return err
	}

	go func() {
		_, _ = io.Copy(io.MultiWriter(original, buffer), r)
	}()
	return nil
}
