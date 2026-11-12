package main

import (
	"time"
)

// --- Shutdown ------------------------------------------------------------

func (u *mainUI) handleClose() {
	if u.closePending {
		if u.d == nil || u.d.shutdown() {
			pDestroyWindow.Call(u.hwnd)
		}
		return
	}

	u.closePending = true
	u.closeSettings()
	u.toggleTunDisplay(false, false)

	if u.isSystemProxyActive {
		u.toggleSystemProxy(false, false, false)
	}

	// Drop the icon before the core stops: Explorer keeps stale icons when the
	// owning window disappears mid-shutdown.
	u.removeTrayIcon()
	u.startShutdownWatchdog()

	if u.d == nil || u.d.shutdown() {
		pDestroyWindow.Call(u.hwnd)
	}
}

func (u *mainUI) endSession() {
	u.closePending = true
	u.closeSettings()
	u.toggleTunDisplay(false, false)

	if u.isSystemProxyActive {
		u.toggleSystemProxy(false, false, false)
	}
	u.removeTrayIcon()
	if u.d != nil {
		// Best effort only: blocking here can trigger the "app is preventing
		// shutdown" prompt. The job object kills the core when this process
		// goes away.
		u.d.shutdown()
	}
}

func (u *mainUI) startShutdownWatchdog() {
	time.AfterFunc(shutdownWatchdogDelay, func() {
		u.post(func() { u.destroyWindow() })
	})
}

// destroyWindow is idempotent; WM_DESTROY removes the tray icon and posts the
// quit message.
func (u *mainUI) destroyWindow() {
	if u.hwnd == 0 {
		return
	}
	if r, _, _ := pIsWindow.Call(u.hwnd); r == 0 {
		return
	}
	pDestroyWindow.Call(u.hwnd)
}
