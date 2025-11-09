package webview

// WindowStateType describes how the window is currently presented.
type WindowStateType int

const (
	// WindowStateUnknown indicates that the current state cannot be determined or has not been initialised.
	WindowStateUnknown WindowStateType = iota
	// WindowStateNormal indicates that the window is shown normally.
	WindowStateNormal
	// WindowStateMinimized indicates that the window has been minimised.
	WindowStateMinimized
	// WindowStateMaximized indicates that the window has been maximised.
	WindowStateMaximized
	// WindowStateFullscreen indicates that the window is displayed fullscreen.
	WindowStateFullscreen
)

// WindowState keeps track of the window geometry and presentation state.
type WindowState struct {
	X      int
	Y      int
	Width  int
	Height int
	State  WindowStateType
}
