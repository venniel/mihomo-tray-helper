package main

import (
	"runtime"
	"time"
	"unsafe"
)

// --- Tray icon -----------------------------------------------------------

// trayTip reports the proxying that is actually in effect. The idle state shows
// the application name, while an active mode is prefixed with the core name. TUN
// only counts while the core runs the TUN config, so a failed or pending start
// never leaves a stale mode on display.
func (u *mainUI) trayTip() string {
	tun := u.tunActive()

	status := ""
	switch {
	case tun && u.isSystemProxyActive:
		status = "TUN 模式 + 系统代理"
	case tun:
		status = "TUN 模式"
	case u.isSystemProxyActive:
		status = "系统代理"
	}
	if status == "" {
		return appTitle
	}
	return "mihomo:" + status
}

func (u *mainUI) addTrayIcon() {
	if u.hwnd == 0 {
		return
	}

	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = u.hwnd
	nid.UID = trayIconID
	nid.UFlags = nifMessage | nifIcon | nifTip | nifShowTip
	nid.UCallbackMessage = wmAppTray
	nid.HIcon = u.hTrayIcon
	copyToUTF16Array(nid.SzTip[:], u.trayTip())

	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		// Explorer may still hold a previous icon for this window after a
		// taskbar restart, which makes NIM_ADD fail. Clear it and retry so the
		// icon and u.trayAdded never disagree.
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		r, _, _ = pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	}
	if r == 0 {
		return
	}
	u.trayAdded = true

	nid.UVersion = notifyVer4
	pShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

func (u *mainUI) updateTrayIcon() {
	if !u.trayAdded {
		return
	}

	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = u.hwnd
	nid.UID = trayIconID
	nid.UFlags = nifIcon | nifTip | nifShowTip
	nid.HIcon = u.hTrayIcon
	copyToUTF16Array(nid.SzTip[:], u.trayTip())
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

func (u *mainUI) removeTrayIcon() {
	if u.hwnd == 0 || !u.trayAdded {
		return
	}

	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = u.hwnd
	nid.UID = trayIconID
	for attempt := 0; ; attempt++ {
		r, _, _ := pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		if r != 0 || attempt >= 2 {
			break
		}
		// The shell rejects deletion while it is busy; retry briefly so the
		// icon is not left behind once the process is gone.
		time.Sleep(50 * time.Millisecond)
	}
	runtime.KeepAlive(&nid)
	u.trayAdded = false
}

func (u *mainUI) showBalloon(text, title string, flags uint32, timeout int) {
	if !u.trayAdded || text == "" {
		return
	}
	if flags == niifError && title == "" {
		title = "Error"
	}

	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = u.hwnd
	nid.UID = trayIconID
	nid.UFlags = nifInfo
	copyToUTF16Array(nid.SzInfo[:], text)
	copyToUTF16Array(nid.SzInfoTitle[:], title)
	nid.DwInfoFlags = flags
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	runtime.KeepAlive(&nid)
}

func (u *mainUI) handleTray(wparam, lparam uintptr) {
	event := loword(lparam)
	switch event {
	case wmContextMenu, wmRightButtonUp:
		u.showTrayMenu()
	case ninSelect, ninKeySelect, wmLeftButtonUp, ninBalloonUserClick:
		u.showSettings()
	}
}
