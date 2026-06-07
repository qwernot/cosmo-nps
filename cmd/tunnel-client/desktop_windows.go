//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	cwUseDefault = -2147483648

	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsVScroll          = 0x00200000
	wsTabStop          = 0x00010000

	esLeft        = 0x0000
	esPassword    = 0x0020
	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esReadOnly    = 0x0800

	bsPushButton = 0x00000000

	wmDestroy = 0x0002
	wmCommand = 0x0111
	wmSetText = 0x000c

	swShow = 5

	idServer = 1001
	idUser   = 1002
	idPass   = 1003
	idStart  = 1004
	idStop   = 1005
	idLogs   = 1006
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procGetWindowTextLen = user32.NewProc("GetWindowTextLengthW")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	desktopAppsMu sync.Mutex
	desktopApps   = map[syscall.Handle]*desktopApp{}
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type desktopApp struct {
	hwnd      syscall.Handle
	server    syscall.Handle
	user      syscall.Handle
	password  syscall.Handle
	logs      syscall.Handle
	cmd       *exec.Cmd
	logText   string
	logMu     sync.Mutex
	runningMu sync.Mutex
	running   bool
}

func runDesktopLauncher(_ string, controlURL string, refresh time.Duration) error {
	cfg := loadLauncherConfig()
	if cfg.ControlURL == "" {
		cfg.ControlURL = controlURL
	}
	if cfg.Refresh == "" {
		cfg.Refresh = refresh.String()
	}

	instance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("TunnelClientDesktop")
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:    syscall.NewCallback(desktopWndProc),
		Instance:   syscall.Handle(instance),
		Background: syscall.Handle(6),
		ClassName:  className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return fmt.Errorf("register window class: %w", err)
	}

	hwnd := createWindow(0, "TunnelClientDesktop", "Tunnel Client", wsOverlappedWindow|wsVisible, cwUseDefault, cwUseDefault, 760, 560, 0, 0, syscall.Handle(instance), 0)
	if hwnd == 0 {
		return fmt.Errorf("create main window failed")
	}
	app := &desktopApp{hwnd: hwnd}
	desktopAppsMu.Lock()
	desktopApps[hwnd] = app
	desktopAppsMu.Unlock()
	app.createControls(cfg)

	procShowWindow.Call(uintptr(hwnd), swShow)
	procUpdateWindow.Call(uintptr(hwnd))

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func desktopWndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCommand:
		id := int(wParam & 0xffff)
		if app := desktopAppFor(hwnd); app != nil {
			switch id {
			case idStart:
				app.start()
			case idStop:
				app.stop()
			}
		}
		return 0
	case wmDestroy:
		if app := desktopAppFor(hwnd); app != nil {
			app.stop()
		}
		desktopAppsMu.Lock()
		delete(desktopApps, hwnd)
		desktopAppsMu.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func desktopAppFor(hwnd syscall.Handle) *desktopApp {
	desktopAppsMu.Lock()
	defer desktopAppsMu.Unlock()
	return desktopApps[hwnd]
}

func (a *desktopApp) createControls(cfg launcherConfig) {
	createLabel(a.hwnd, "总控地址", 24, 24, 90, 24)
	a.server = createEdit(a.hwnd, cfg.ControlURL, 118, 20, 560, 28, false, false)
	createLabel(a.hwnd, "用户名", 24, 66, 90, 24)
	a.user = createEdit(a.hwnd, cfg.User, 118, 62, 220, 28, false, false)
	createLabel(a.hwnd, "密码", 360, 66, 70, 24)
	a.password = createEdit(a.hwnd, "", 420, 62, 258, 28, true, false)
	createButton(a.hwnd, "启动", 118, 106, 100, 34, idStart)
	createButton(a.hwnd, "停止", 232, 106, 100, 34, idStop)
	createLabel(a.hwnd, "运行日志", 24, 158, 100, 24)
	a.logs = createEdit(a.hwnd, "等待启动...", 24, 184, 688, 320, false, true)
}

func (a *desktopApp) start() {
	a.runningMu.Lock()
	if a.running {
		a.runningMu.Unlock()
		a.appendLog("launcher: 已在运行")
		return
	}
	cfg := launcherConfig{
		ControlURL: strings.TrimSpace(getWindowText(a.server)),
		User:       strings.TrimSpace(getWindowText(a.user)),
		Password:   getWindowText(a.password),
		Refresh:    "30s",
	}
	if cfg.ControlURL == "" || cfg.User == "" || cfg.Password == "" {
		a.runningMu.Unlock()
		a.appendLog("launcher: 总控地址、用户名、密码都必须填写")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		a.runningMu.Unlock()
		a.appendLog("launcher: " + err.Error())
		return
	}
	cmd := exec.Command(exe, "-no-gui", "-server", cfg.ControlURL, "-user", cfg.User, "-password", cfg.Password)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.runningMu.Unlock()
		a.appendLog("launcher: " + err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.runningMu.Unlock()
		a.appendLog("launcher: " + err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		a.runningMu.Unlock()
		a.appendLog("launcher: " + err.Error())
		return
	}
	a.cmd = cmd
	a.running = true
	a.runningMu.Unlock()

	_ = saveLauncherConfig(cfg)
	a.appendLog("launcher: tunnel-client 已启动")
	go a.capture(stdout)
	go a.capture(stderr)
	go a.wait(cmd)
}

func (a *desktopApp) stop() {
	a.runningMu.Lock()
	cmd := a.cmd
	if !a.running || cmd == nil || cmd.Process == nil {
		a.runningMu.Unlock()
		return
	}
	a.runningMu.Unlock()
	a.appendLog("launcher: 正在停止")
	_ = cmd.Process.Kill()
}

func (a *desktopApp) capture(pipe any) {
	reader, ok := pipe.(*os.File)
	if !ok {
		return
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		a.appendLog(scanner.Text())
	}
}

func (a *desktopApp) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	a.runningMu.Lock()
	if a.cmd == cmd {
		a.cmd = nil
		a.running = false
	}
	a.runningMu.Unlock()
	if err != nil {
		a.appendLog("launcher: tunnel-client 已退出: " + err.Error())
	} else {
		a.appendLog("launcher: tunnel-client 已停止")
	}
}

func (a *desktopApp) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logText += time.Now().Format("15:04:05") + " " + line + "\r\n"
	if len(a.logText) > 24000 {
		a.logText = a.logText[len(a.logText)-24000:]
	}
	setWindowText(a.logs, a.logText)
}

func createLabel(parent syscall.Handle, text string, x, y, w, h int32) syscall.Handle {
	return createWindow(0, "STATIC", text, wsChild|wsVisible, x, y, w, h, parent, 0, 0, 0)
}

func createEdit(parent syscall.Handle, text string, x, y, w, h int32, password, multiline bool) syscall.Handle {
	style := uint32(wsChild | wsVisible | wsTabStop | esLeft)
	if password {
		style |= esPassword
	}
	if multiline {
		style |= esMultiline | esAutoVScroll | esReadOnly | wsVScroll
	}
	return createWindow(0x00000200, "EDIT", text, style, x, y, w, h, parent, 0, 0, 0)
}

func createButton(parent syscall.Handle, text string, x, y, w, h int32, id uintptr) syscall.Handle {
	return createWindow(0, "BUTTON", text, wsChild|wsVisible|wsTabStop|bsPushButton, x, y, w, h, parent, syscall.Handle(id), 0, 0)
}

func createWindow(exStyle uint32, className, title string, style uint32, x, y, w, h int32, parent, menu, instance syscall.Handle, param uintptr) syscall.Handle {
	ret, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(utf16Ptr(className))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		uintptr(style),
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		uintptr(parent),
		uintptr(menu),
		uintptr(instance),
		param,
	)
	return syscall.Handle(ret)
}

func getWindowText(hwnd syscall.Handle) string {
	n, _, _ := procGetWindowTextLen.Call(uintptr(hwnd))
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func setWindowText(hwnd syscall.Handle, text string) {
	procSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func utf16Ptr(text string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(text)
	return ptr
}
