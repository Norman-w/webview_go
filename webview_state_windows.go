//go:build windows

package webview

import (
	"sync"
	"syscall"
	"unsafe"
)

const (
	swShowMaximized   = 3
	swShowMinimized   = 2
	swShowMinNoActive = 7

	monitorDefaultToNearest = 2

	gwlStyle           = -16
	wsOverlappedWindow = 0x00CF0000
	swpNoSize          = 0x0001
	swpNoMove          = 0x0002
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
)

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type point struct {
	X int32
	Y int32
}

type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    point
	PtMaxPosition    point
	RcNormalPosition rect
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
	procGetWindowLongPtrW  = user32.NewProc("GetWindowLongPtrW")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW    = user32.NewProc("CallWindowProcW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
)

const (
	wmDestroy          = 0x0002
	wmNCDestroy        = 0x0082
	wmMove             = 0x0003
	wmSize             = 0x0005
	wmWindowPosChanged = 0x0047

	sizeRestored  = 0
	sizeMinimized = 1
	sizeMaximized = 2

	gwlWndProc int32 = -4
)

type windowPos struct {
	hwnd            uintptr
	hwndInsertAfter uintptr
	x               int32
	y               int32
	cx              int32
	cy              int32
	flags           uint32
}

var (
	windowSubclassMu sync.RWMutex
	windowSubclass   = map[uintptr]*webview{}

	windowProcCallback = syscall.NewCallback(windowProc)
)

func getWindowState(w *webview) WindowState {
	ensureWindowHook(w)

	state := WindowState{State: WindowStateUnknown}
	hwnd := uintptr(w.Window())
	if hwnd == 0 {
		return state
	}

	var r rect
	if callGetWindowRect(hwnd, &r) {
		state.X = int(r.Left)
		state.Y = int(r.Top)
		state.Width = int(r.Right - r.Left)
		state.Height = int(r.Bottom - r.Top)
	}

	placement := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if callGetWindowPlacement(hwnd, &placement) {
		switch placement.ShowCmd {
		case swShowMaximized:
			state.State = WindowStateMaximized
		case swShowMinimized, swShowMinNoActive:
			state.State = WindowStateMinimized
		default:
			state.State = WindowStateNormal
		}
	} else if state.State == WindowStateUnknown {
		state.State = WindowStateNormal
	}

	if state.State != WindowStateFullscreen {
		style := callGetWindowLongPtr(hwnd, gwlStyle)
		if style != 0 && (style&wsOverlappedWindow) == 0 {
			mon := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
			if monitor := callMonitorFromWindow(hwnd, monitorDefaultToNearest); monitor != 0 {
				if callGetMonitorInfo(monitor, &mon) {
					if state.Width >= int(mon.Monitor.Right-mon.Monitor.Left) &&
						state.Height >= int(mon.Monitor.Bottom-mon.Monitor.Top) &&
						state.X <= int(mon.Monitor.Left) &&
						state.Y <= int(mon.Monitor.Top) {
						state.State = WindowStateFullscreen
					}
				}
			}
		}
	}

	return state
}

func callGetWindowRect(hwnd uintptr, rect *rect) bool {
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(rect)))
	return ret != 0
}

func callGetWindowPlacement(hwnd uintptr, placement *windowPlacement) bool {
	ret, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(placement)))
	return ret != 0
}

func callMonitorFromWindow(hwnd uintptr, flags uint32) uintptr {
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(flags))
	return monitor
}

func callGetMonitorInfo(monitor uintptr, info *monitorInfo) bool {
	ret, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(info)))
	return ret != 0
}

func callGetWindowLongPtr(hwnd uintptr, index int32) uintptr {
	ret, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(index))
	return ret
}

func setWindowPosition(w *webview, x, y int) {
	ensureWindowHook(w)

	if w == nil {
		return
	}

	w.updateCachedState(func(state *WindowState) {
		state.X = x
		state.Y = y
		if state.State == WindowStateUnknown {
			state.State = WindowStateNormal
		}
	})

	hwnd := uintptr(w.Window())
	if hwnd == 0 {
		return
	}
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func setWindowSize(w *webview, width, height int) {
	ensureWindowHook(w)

	if w == nil {
		return
	}

	w.updateCachedState(func(state *WindowState) {
		if width > 0 {
			state.Width = width
		}
		if height > 0 {
			state.Height = height
		}
		if state.State == WindowStateUnknown && state.Width > 0 && state.Height > 0 {
			state.State = WindowStateNormal
		}
	})

	hwnd := uintptr(w.Window())
	if hwnd == 0 || width <= 0 || height <= 0 {
		return
	}
	procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(width), uintptr(height), swpNoMove|swpNoZOrder|swpNoActivate)
}

func ensureWindowHook(w *webview) {
	if w == nil {
		return
	}

	hwnd := uintptr(w.Window())
	if hwnd == 0 {
		return
	}

	w.hookMu.Lock()
	defer w.hookMu.Unlock()

	if w.hookInstalled {
		return
	}

	orig, _, err := procSetWindowLongPtrW.Call(hwnd, toUintptrIndex(gwlWndProc), windowProcCallback)
	if orig == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno != 0 {
			return
		}
	}

	w.origWndProc = orig
	w.hwnd = hwnd
	w.hookInstalled = true

	windowSubclassMu.Lock()
	windowSubclass[hwnd] = w
	windowSubclassMu.Unlock()
}

func removeWindowHook(w *webview) {
	if w == nil {
		return
	}

	w.hookMu.Lock()
	defer w.hookMu.Unlock()

	if !w.hookInstalled {
		return
	}

	hwnd := w.hwnd
	if hwnd != 0 && w.origWndProc != 0 {
		procSetWindowLongPtrW.Call(hwnd, toUintptrIndex(gwlWndProc), w.origWndProc)
	}

	windowSubclassMu.Lock()
	delete(windowSubclass, hwnd)
	windowSubclassMu.Unlock()

	w.hookInstalled = false
	w.origWndProc = 0
	w.hwnd = 0
}

func windowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	windowSubclassMu.RLock()
	w := windowSubclass[hwnd]
	windowSubclassMu.RUnlock()

	var orig uintptr
	if w != nil {
		w.hookMu.Lock()
		orig = w.origWndProc
		w.hookMu.Unlock()
	}

	if w != nil {
		switch msg {
		case wmSize:
			handleSizeMessage(w, wparam, lparam)
		case wmWindowPosChanged:
			handleWindowPosChanged(w, lparam)
		case wmMove:
			handleMoveMessage(w, lparam)
		case wmDestroy, wmNCDestroy:
			ret := forwardWindowMessage(orig, hwnd, uintptr(msg), wparam, lparam)
			removeWindowHook(w)
			return ret
		}
	}

	return forwardWindowMessage(orig, hwnd, uintptr(msg), wparam, lparam)
}

func handleSizeMessage(w *webview, wparam, lparam uintptr) {
	width := int(int32(uint16(lparam & 0xFFFF)))
	height := int(int32(uint16((lparam >> 16) & 0xFFFF)))

	if width <= 0 || height <= 0 {
		return
	}

	newState := WindowStateNormal
	switch uint32(wparam) {
	case sizeMaximized:
		newState = WindowStateMaximized
	case sizeMinimized:
		newState = WindowStateMinimized
	default:
		newState = WindowStateNormal
	}

	w.updateCachedState(func(state *WindowState) {
		state.Width = width
		state.Height = height
		state.State = newState
	})
}

func handleWindowPosChanged(w *webview, lparam uintptr) {
	if lparam == 0 {
		return
	}
	pos := (*windowPos)(unsafe.Pointer(lparam))

	if pos == nil {
		return
	}

	w.updateCachedState(func(state *WindowState) {
		if pos.flags&swpNoMove == 0 {
			state.X = int(pos.x)
			state.Y = int(pos.y)
			if state.State == WindowStateUnknown {
				state.State = WindowStateNormal
			}
		}
		if pos.flags&swpNoSize == 0 {
			if pos.cx > 0 {
				state.Width = int(pos.cx)
			}
			if pos.cy > 0 {
				state.Height = int(pos.cy)
			}
			if state.State == WindowStateUnknown {
				state.State = WindowStateNormal
			}
		}
	})
}

func handleMoveMessage(w *webview, lparam uintptr) {
	x := int(int32(int16(uint16(lparam & 0xFFFF))))
	y := int(int32(int16(uint16((lparam >> 16) & 0xFFFF))))

	w.updateCachedState(func(state *WindowState) {
		state.X = x
		state.Y = y
		if state.State == WindowStateUnknown {
			state.State = WindowStateNormal
		}
	})
}

func forwardWindowMessage(orig, hwnd, msg, wparam, lparam uintptr) uintptr {
	if orig != 0 {
		ret, _, _ := procCallWindowProcW.Call(orig, hwnd, uintptr(msg), wparam, lparam)
		return ret
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func toUintptrIndex(index int32) uintptr {
	return uintptr(uint32(index))
}
