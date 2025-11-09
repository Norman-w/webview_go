//go:build !windows

package webview

func getWindowState(*webview) WindowState {
	return WindowState{State: WindowStateUnknown}
}

func setWindowPosition(w *webview, x, y int) {
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
}

func setWindowSize(w *webview, width, height int) {
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
}

func ensureWindowHook(*webview) {}

func removeWindowHook(*webview) {}
