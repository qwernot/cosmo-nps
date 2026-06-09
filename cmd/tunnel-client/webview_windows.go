//go:build windows

package main

import (
	"log"
	"runtime"
	"sync"
	"syscall"

	"github.com/jchv/go-webview2"
)

var (
	webviewMu     sync.Mutex
	activeWebView webview2.WebView
	lpPrevWndProc uintptr
)

var (
	// Reusing package-level user32 and kernel32 LazyDLLs from autostart_windows.go
	pSendMessageW        = user32.NewProc("SendMessageW")
	pLoadIconW           = user32.NewProc("LoadIconW")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pSetActiveWindow     = user32.NewProc("SetActiveWindow")
	pSetWindowPos        = user32.NewProc("SetWindowPos")
	pSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	pSetWindowLongW           = user32.NewProc("SetWindowLongW")
	pCallWindowProcW          = user32.NewProc("CallWindowProcW")
	pAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
)

const (
	WM_SETICON = 0x0080
	ICON_SMALL = 0
	ICON_BIG   = 1
)

func setWindowIcon(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	hInst, _, _ := pGetModuleHandleW.Call(0)
	if hInst == 0 {
		return
	}

	// Try common resource IDs for the icon compiled into the executable
	for _, resId := range []uintptr{1, 7, 101, 14, 32512} {
		hIcon, _, _ := pLoadIconW.Call(hInst, resId)
		if hIcon != 0 {
			pSendMessageW.Call(hwnd, WM_SETICON, ICON_SMALL, hIcon)
			pSendMessageW.Call(hwnd, WM_SETICON, ICON_BIG, hIcon)
			break
		}
	}
}

func setWindowLongPtr(hwnd uintptr, index int32, value uintptr) uintptr {
	if err := pSetWindowLongPtrW.Find(); err == nil {
		ret, _, _ := pSetWindowLongPtrW.Call(hwnd, uintptr(index), value)
		return ret
	}
	ret, _, _ := pSetWindowLongW.Call(hwnd, uintptr(index), value)
	return ret
}

func allowSetForegroundWindow() {
	const ASFW_ANY = ^uintptr(0) // -1
	pAllowSetForegroundWindow.Call(ASFW_ANY)
}

func subWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	const WM_CLOSE = 0x0010
	if msg == WM_CLOSE {
		// Hide the window instead of closing/destroying it
		const SW_HIDE = 0
		pShowWindow.Call(hwnd, SW_HIDE)
		return 0
	}
	// Call original wndproc
	ret, _, _ := pCallWindowProcW.Call(lpPrevWndProc, hwnd, msg, wParam, lParam)
	return ret
}

func openDashboardWindow(url string) {
	webviewMu.Lock()
	defer webviewMu.Unlock()

	if activeWebView != nil {
		hwnd := uintptr(activeWebView.Window())
		if hwnd != 0 {
			const (
				SW_SHOW        = 5
				SW_RESTORE     = 9
				HWND_TOPMOST   = ^uintptr(0) // -1
				HWND_NOTOPMOST = ^uintptr(1) // -2
				SWP_NOSIZE     = 0x0001
				SWP_NOMOVE     = 0x0002
				SWP_SHOWWINDOW = 0x0040
			)
			// Show and restore window
			pShowWindow.Call(hwnd, SW_SHOW)
			pShowWindow.Call(hwnd, SW_RESTORE)

			// Temporarily make it topmost to force to the front, then reset
			pSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)
			pSetWindowPos.Call(hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW)

			pSetForegroundWindow.Call(hwnd)
			pSetActiveWindow.Call(hwnd)
		}
		activeWebView.Navigate(url)
		return
	}

	go func() {
		runtime.LockOSThread()

		w := webview2.New(true)
		if w == nil {
			log.Printf("Failed to create WebView2 window, falling back to default browser.")
			go openBrowser(url)
			return
		}

		hwnd := uintptr(w.Window())

		// Apply the application icon to the window
		setWindowIcon(hwnd)

		webviewMu.Lock()
		activeWebView = w
		// Subclass the window
		lpPrevWndProc = setWindowLongPtr(hwnd, -4, syscall.NewCallback(subWndProc))
		webviewMu.Unlock()

		defer func() {
			webviewMu.Lock()
			activeWebView = nil
			if lpPrevWndProc != 0 {
				setWindowLongPtr(hwnd, -4, lpPrevWndProc)
				lpPrevWndProc = 0
			}
			w.Destroy()
			webviewMu.Unlock()
		}()

		w.SetTitle("Tunnel Client Dashboard")
		w.SetSize(1000, 680, webview2.HintNone)
		w.Navigate(url)
		w.Run()
	}()
}
