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

	bsPushButton = 0

	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmPaint       = 0x000f
	wmCtlColorMsg = 0x0138
	wmSetText     = 0x000c
	swShow        = 5

	idServer = 1001
	idUser   = 1002
	idPass   = 1003
	idStart  = 1004
	idStop   = 1005
	idLogs   = 1006
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	gdi32   = syscall.NewLazyDLL("gdi32.dll")
	kernel  = syscall.NewLazyDLL("kernel32.dll")
	desktop = map[syscall.Handle]*nativeApp{}
	appMu   sync.Mutex

	registerClassEx = user32.NewProc("RegisterClassExW")
	createWindowEx  = user32.NewProc("CreateWindowExW")
	defWindowProc   = user32.NewProc("DefWindowProcW")
	showWindow      = user32.NewProc("ShowWindow")
	updateWindow    = user32.NewProc("UpdateWindow")
	getMessage      = user32.NewProc("GetMessageW")
	translateMsg    = user32.NewProc("TranslateMessage")
	dispatchMsg     = user32.NewProc("DispatchMessageW")
	postQuit        = user32.NewProc("PostQuitMessage")
	setWindowText   = user32.NewProc("SetWindowTextW")
	getText         = user32.NewProc("GetWindowTextW")
	getTextLen      = user32.NewProc("GetWindowTextLengthW")
	beginPaint      = user32.NewProc("BeginPaint")
	endPaint        = user32.NewProc("EndPaint")
	invalidateRect  = user32.NewProc("InvalidateRect")

	getModuleHandle = kernel.NewProc("GetModuleHandleW")

	createSolidBrush = gdi32.NewProc("CreateSolidBrush")
	setBkColor       = gdi32.NewProc("SetBkColor")
	setTextColor     = gdi32.NewProc("SetTextColor")
	rectangle        = gdi32.NewProc("Rectangle")
	createFont       = gdi32.NewProc("CreateFontW")
	selectObject     = gdi32.NewProc("SelectObject")
	textOut          = gdi32.NewProc("TextOutW")
)

type wndClass struct {
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

type point struct{ X, Y int32 }
type msg struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	Hdc         syscall.Handle
	Erase       int32
	Paint       rect
	Restore     int32
	IncUpdate   int32
	RGBReserved [32]byte
}

type nativeApp struct {
	hwnd     syscall.Handle
	server   syscall.Handle
	user     syscall.Handle
	password syscall.Handle
	logs     syscall.Handle
	status   string
	cmd      *exec.Cmd
	running  bool
	logText  string
	mu       sync.Mutex
}

func runDesktopLauncher(_ string, controlURL string, refresh time.Duration) error {
	cfg := loadLauncherConfig()
	if cfg.ControlURL == "" {
		cfg.ControlURL = controlURL
	}
	if cfg.Refresh == "" {
		cfg.Refresh = refresh.String()
	}
	instance, _, _ := getModuleHandle.Call(0)
	className := utf16("TunnelClientNative")
	wc := wndClass{
		Size:       uint32(unsafe.Sizeof(wndClass{})),
		WndProc:    syscall.NewCallback(wndProc),
		Instance:   syscall.Handle(instance),
		Background: syscall.Handle(0),
		ClassName:  className,
	}
	if ret, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("register window: %w", err)
	}
	hwnd := createWin(0, "TunnelClientNative", "Tunnel Client", wsOverlappedWindow|wsVisible, cwUseDefault, cwUseDefault, 1000, 640, 0, 0, syscall.Handle(instance), 0)
	if hwnd == 0 {
		return fmt.Errorf("create window failed")
	}
	app := &nativeApp{hwnd: hwnd, status: "未连接"}
	appMu.Lock()
	desktop[hwnd] = app
	appMu.Unlock()
	app.createControls(cfg)
	showWindow.Call(uintptr(hwnd), swShow)
	updateWindow.Call(uintptr(hwnd))
	var m msg
	for {
		ret, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		translateMsg.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMsg.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func wndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmPaint:
		if app := appFor(hwnd); app != nil {
			app.paint()
			return 0
		}
	case wmCtlColorMsg:
		hdc := syscall.Handle(wParam)
		setTextColor.Call(uintptr(hdc), uintptr(rgb(220, 231, 245)))
		setBkColor.Call(uintptr(hdc), uintptr(rgb(15, 23, 42)))
		return uintptr(brush(rgb(15, 23, 42)))
	case wmCommand:
		if app := appFor(hwnd); app != nil {
			switch int(wParam & 0xffff) {
			case idStart:
				app.start()
			case idStop:
				app.stop()
			}
		}
		return 0
	case wmDestroy:
		if app := appFor(hwnd); app != nil {
			app.stop()
		}
		appMu.Lock()
		delete(desktop, hwnd)
		appMu.Unlock()
		postQuit.Call(0)
		return 0
	}
	ret, _, _ := defWindowProc.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func appFor(hwnd syscall.Handle) *nativeApp {
	appMu.Lock()
	defer appMu.Unlock()
	return desktop[hwnd]
}

func (a *nativeApp) createControls(cfg launcherConfig) {
	createLabel(a.hwnd, "总控地址", 220, 98, 82, 24)
	a.server = createEdit(a.hwnd, cfg.ControlURL, 306, 94, 520, 30, false, false)
	createLabel(a.hwnd, "用户名", 220, 144, 82, 24)
	a.user = createEdit(a.hwnd, cfg.User, 306, 140, 220, 30, false, false)
	createLabel(a.hwnd, "密码", 550, 144, 54, 24)
	a.password = createEdit(a.hwnd, "", 610, 140, 216, 30, true, false)
	createButton(a.hwnd, "连接", 846, 94, 86, 36, idStart)
	createButton(a.hwnd, "停止", 846, 140, 86, 34, idStop)
	createLabel(a.hwnd, "连接日志", 220, 224, 100, 24)
	a.logs = createEdit(a.hwnd, "等待连接...", 220, 252, 732, 300, false, true)
}

func (a *nativeApp) paint() {
	var ps paintStruct
	hdc, _, _ := beginPaint.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(&ps)))
	defer endPaint.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(&ps)))
	fill(hdc, 0, 0, 1000, 640, rgb(15, 23, 42))
	fill(hdc, 0, 0, 190, 640, rgb(8, 13, 20))
	fill(hdc, 12, 72, 174, 116, rgb(42, 135, 230))
	fill(hdc, 204, 34, 952, 62, rgb(21, 32, 48))
	fill(hdc, 204, 80, 952, 196, rgb(24, 36, 54))
	fill(hdc, 204, 212, 952, 572, rgb(24, 36, 54))
	fontBig := font(24, true)
	fontNormal := font(16, false)
	selectObject.Call(hdc, uintptr(fontBig))
	drawText(hdc, 24, 22, "Tunnel Client")
	selectObject.Call(hdc, uintptr(fontNormal))
	drawText(hdc, 28, 84, "客户端")
	drawText(hdc, 28, 136, "连接日志")
	drawText(hdc, 28, 188, "设置")
	drawText(hdc, 220, 42, "NPS 云穿透客户端")
	drawText(hdc, 740, 42, "状态: "+a.status)
}

func (a *nativeApp) start() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		a.appendLog("客户端已在运行")
		return
	}
	cfg := launcherConfig{ControlURL: strings.TrimSpace(text(a.server)), User: strings.TrimSpace(text(a.user)), Password: text(a.password), Refresh: "30s"}
	if cfg.ControlURL == "" || cfg.User == "" || cfg.Password == "" {
		a.mu.Unlock()
		a.appendLog("请填写总控地址、用户名和密码")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		a.mu.Unlock()
		a.appendLog(err.Error())
		return
	}
	cmd := exec.Command(exe, "-no-gui", "-server", cfg.ControlURL, "-user", cfg.User, "-password", cfg.Password)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.mu.Unlock()
		a.appendLog(err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.mu.Unlock()
		a.appendLog(err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		a.mu.Unlock()
		a.appendLog(err.Error())
		return
	}
	a.cmd = cmd
	a.running = true
	a.status = "连接中"
	a.mu.Unlock()
	_ = saveLauncherConfig(cfg)
	a.appendLog("正在连接总控 " + cfg.ControlURL)
	invalidateRect.Call(uintptr(a.hwnd), 0, 1)
	go a.capture(stdout)
	go a.capture(stderr)
	go a.wait(cmd)
}

func (a *nativeApp) stop() {
	a.mu.Lock()
	cmd := a.cmd
	if !a.running || cmd == nil || cmd.Process == nil {
		a.mu.Unlock()
		return
	}
	a.status = "停止中"
	a.mu.Unlock()
	a.appendLog("正在停止客户端")
	_ = cmd.Process.Kill()
	invalidateRect.Call(uintptr(a.hwnd), 0, 1)
}

func (a *nativeApp) capture(pipe any) {
	file, ok := pipe.(*os.File)
	if !ok {
		return
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "logged in") || strings.Contains(line, "Successful connection") {
			a.mu.Lock()
			a.status = "已连接"
			a.mu.Unlock()
			invalidateRect.Call(uintptr(a.hwnd), 0, 1)
		}
		a.appendLog(line)
	}
}

func (a *nativeApp) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	a.mu.Lock()
	if a.cmd == cmd {
		a.cmd = nil
		a.running = false
		a.status = "未连接"
	}
	a.mu.Unlock()
	if err != nil {
		a.appendLog("客户端已退出: " + err.Error())
	} else {
		a.appendLog("客户端已停止")
	}
	invalidateRect.Call(uintptr(a.hwnd), 0, 1)
}

func (a *nativeApp) appendLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.mu.Lock()
	a.logText += time.Now().Format("15:04:05") + "  " + line + "\r\n"
	if len(a.logText) > 24000 {
		a.logText = a.logText[len(a.logText)-24000:]
	}
	out := a.logText
	a.mu.Unlock()
	setText(a.logs, out)
}

func createLabel(parent syscall.Handle, value string, x, y, w, h int32) syscall.Handle {
	return createWin(0, "STATIC", value, wsChild|wsVisible, x, y, w, h, parent, 0, 0, 0)
}
func createEdit(parent syscall.Handle, value string, x, y, w, h int32, password, multiline bool) syscall.Handle {
	style := uint32(wsChild | wsVisible | wsTabStop | esLeft)
	if password {
		style |= esPassword
	}
	if multiline {
		style |= esMultiline | esAutoVScroll | esReadOnly | wsVScroll
	}
	return createWin(0x200, "EDIT", value, style, x, y, w, h, parent, 0, 0, 0)
}
func createButton(parent syscall.Handle, value string, x, y, w, h int32, id uintptr) syscall.Handle {
	return createWin(0, "BUTTON", value, wsChild|wsVisible|wsTabStop|bsPushButton, x, y, w, h, parent, syscall.Handle(id), 0, 0)
}
func createWin(ex uint32, class, title string, style uint32, x, y, w, h int32, parent, menu, instance syscall.Handle, param uintptr) syscall.Handle {
	ret, _, _ := createWindowEx.Call(uintptr(ex), uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(title))), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(parent), uintptr(menu), uintptr(instance), param)
	return syscall.Handle(ret)
}
func text(hwnd syscall.Handle) string {
	n, _, _ := getTextLen.Call(uintptr(hwnd))
	buf := make([]uint16, n+1)
	getText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}
func setText(hwnd syscall.Handle, value string) {
	setWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16(value))))
}
func fill(hdc uintptr, l, t, r, b int32, color uint32) {
	br := brush(color)
	selectObject.Call(hdc, uintptr(br))
	rectangle.Call(hdc, uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}
func drawText(hdc uintptr, x, y int32, value string) {
	chars, _ := syscall.UTF16FromString(value)
	textOut.Call(hdc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1))
}
func font(size int32, bold bool) syscall.Handle {
	weight := int32(400)
	if bold {
		weight = 700
	}
	ret, _, _ := createFont.Call(uintptr(size), 0, 0, 0, uintptr(weight), 0, 0, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16("Microsoft YaHei UI"))))
	return syscall.Handle(ret)
}
func brush(color uint32) syscall.Handle {
	ret, _, _ := createSolidBrush.Call(uintptr(color))
	return syscall.Handle(ret)
}
func rgb(r, g, b byte) uint32 {
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16
}
func utf16(value string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(value)
	return ptr
}
