//go:build windows

package main

import (
	"io"
	"log"
	"os"
)

func setupLogging(buffer *logBuffer) {
	log.SetOutput(io.MultiWriter(os.Stderr, buffer))
}
