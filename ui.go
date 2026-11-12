package main

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mainWindowClass = "MTH-main"
	wmAppDispatch   = wmApp + 2
	trayIconID      = 1

	// The core gets up to ~16s to stop gracefully; force the exit afterwards
	// so a stuck supervisor cannot leave an invisible process behind.
	shutdownWatchdogDelay = 30 * time.Second

	ninBalloonUserClick = 0x0405

	idMenuRestart      = 1
	idMenuAutostart    = 2
	idMenuHomepage     = 3
	idMenuDashboard    = 4
	idMenuQuit         = 5
	idMenuTun          = 6
	idMenuSystemProxy  = 7
	idMenuModeBase     = 100
	idMenuSelectorBase = 10000
)

type selectorAction struct {
	SelectorIndex int
	OutboundIndex int
}

type mainUI struct {
	d            *drover
	hwnd         uintptr
	startupFlags appFlags

	hTrayIcon      uintptr
	hIconLarge     uintptr
	taskbarCreated uint32
	trayAdded      bool

	isTunActive         bool
	isSystemProxyActive bool
	closePending        bool
	tunBusy             bool

	autostartState autostartState
	autostartBusy  bool

	settings           *settingsDialog
	settingsOpening    bool
	settingsClosed     bool
	settingsClosedTick uint32

	lastRequestID int
	pending       map[int]selectorAction
	menuActions   map[uint32]selectorAction
	menuModes     []string

	opsMu sync.Mutex
	ops   []func()
}

var mainWindowProc = windows.NewCallback(mainWndProcFunc)

func mainWndProcFunc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if u := lookupMainWindow(hwnd); u != nil {
		return u.handleMessage(hwnd, msg, wparam, lparam)
	}

	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func newMainUI(d *drover, flags appFlags) (*mainUI, error) {
	u := &mainUI{
		d:            d,
		startupFlags: flags,
		tunBusy:      true,
		pending:      map[int]selectorAction{},
		menuActions:  map[uint32]selectorAction{},
	}

	hInstance := getModuleHandle()
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	u.hIconLarge = loadApplicationIcon(getSystemMetric(smCXIcon), getSystemMetric(smCYIcon))
	smallX := getSystemMetric(smCXSmIcon)
	smallY := getSystemMetric(smCYSmIcon)
	if smallX <= 0 {
		smallX = 16
	}
	if smallY <= 0 {
		smallY = 16
	}
	u.hTrayIcon = loadEmbeddedIcon(iconData, smallX, smallY)
	if u.hTrayIcon == 0 {
		u.hTrayIcon = loadApplicationIcon(smallX, smallY)
	}

	wc := wndClassEx{
		Style:         0,
		LpfnWndProc:   mainWindowProc,
		HInstance:     hInstance,
		HIcon:         u.hIconLarge,
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: utf16Ptr(mainWindowClass),
		HIconSm:       u.hTrayIcon,
	}

	if err := registerWindowClass(&wc, "main"); err != nil {
		return nil, err
	}

	// The message-only window is never shown, but screen readers still list it.
	// Leave its title empty so it is not announced next to the tray icon.
	hwnd := createWindowEx(0, mainWindowClass, "", wsPopup, 0, 0, 0, 0, 0, 0, hInstance, 0)
	if hwnd == 0 {
		return nil, errors.New("Failed to create main window.")
	}
	u.hwnd = hwnd
	registerMainWindow(hwnd, u)

	created, _, _ := pRegisterWindowMsg.Call(uintptr(unsafe.Pointer(utf16Ptr("TaskbarCreated"))))
	u.taskbarCreated = uint32(created)
	if u.taskbarCreated != 0 {
		pChangeMessageFilter.Call(hwnd, uintptr(u.taskbarCreated), msgfltAllow, 0)
	}

	u.initAutostart()
	return u, nil
}

func (u *mainUI) initDrover() {
	u.d.setNotifyHandle(u.hwnd)
	u.isTunActive = u.d.isTunActive

	if u.d.options.SystemProxyAuto {
		u.toggleSystemProxy(true, false, false)
	}

	u.updateTrayIcon()
	u.addTrayIcon()

	u.d.setOnEvent(u.handleDroverEvent)
}

func (u *mainUI) run() {
	var msg winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if u.settings != nil && u.settings.hwnd != 0 {
			if r2, _, _ := pIsDialogMessageW.Call(u.settings.hwnd, uintptr(unsafe.Pointer(&msg))); r2 != 0 {
				continue
			}
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (u *mainUI) post(fn func()) {
	if u.hwnd == 0 {
		return
	}
	u.opsMu.Lock()
	u.ops = append(u.ops, fn)
	u.opsMu.Unlock()
	postMessage(u.hwnd, wmAppDispatch, 0, 0)
}

func (u *mainUI) drainPosted() {
	for {
		u.opsMu.Lock()
		ops := u.ops
		u.ops = nil
		u.opsMu.Unlock()
		if len(ops) == 0 {
			return
		}
		for _, fn := range ops {
			fn()
		}
	}
}

func (u *mainUI) handleMessage(hwnd, msg, wparam, lparam uintptr) uintptr {
	if u.taskbarCreated != 0 && msg == uintptr(u.taskbarCreated) {
		u.trayAdded = false
		if !u.closePending {
			u.addTrayIcon()
		}
		return 0
	}

	switch uint32(msg) {
	case wmAppDispatch:
		u.drainPosted()
		return 0

	case wmAppTray:
		u.handleTray(wparam, lparam)
		return 0

	case wmDroverCanClose:
		if u.closePending {
			pEndMenu.Call()
			postMessage(hwnd, wmClose, 0, 0)
		}
		return 0

	case wmQueryEndSession:
		return 1

	case wmEndSession:
		if wparam != 0 {
			u.endSession()
		}
		return 0

	case wmClose:
		u.handleClose()
		return 0

	case wmDestroy:
		u.cleanup()
		pPostQuitMessage.Call(0)
		return 0

	case wmNcDestroy:
		unregisterMainWindow(hwnd)
	}

	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (u *mainUI) cleanup() {
	u.removeTrayIcon()
	for _, icon := range []uintptr{u.hTrayIcon, u.hIconLarge} {
		if icon != 0 {
			pDestroyIcon.Call(icon)
		}
	}
	u.hTrayIcon = 0
	u.hIconLarge = 0
}

// --- Actions -------------------------------------------------------------

func (u *mainUI) requestClose() {
	postMessage(u.hwnd, wmClose, 0, 0)
}

func (u *mainUI) restartApp() {
	if u.closePending {
		return
	}

	flags := appFlags(1 << afRestart)
	if u.isTunActive {
		flags |= 1 << afTun
	}

	if launchSelf(flagsToCmdLine(flags), u.hwnd, isProcessElevated()) {
		u.requestClose()
	}
}

func (u *mainUI) toggleSystemProxy(enable, updateIcon, persist bool) {
	if enable && u.closePending {
		return
	}

	// Track what Windows actually applied, not what was requested, so the
	// tooltip and menu never claim a state the system is not in.
	if enable {
		u.isSystemProxyActive = u.d.enableSystemProxy()
	} else {
		u.isSystemProxyActive = !u.d.disableSystemProxy()
	}
	if persist {
		u.d.storeSystemProxy(enable)
	}
	if updateIcon {
		u.updateTrayIcon()
	}
}

func (u *mainUI) tunActive() bool {
	return u.isTunActive && u.d != nil && u.d.supervisor != nil && u.d.supervisor.currentState() == csRunning
}

func (u *mainUI) toggleTunDisplay(active, updateIcon bool) {
	u.isTunActive = active
	if updateIcon {
		u.updateTrayIcon()
	}
}

func (u *mainUI) toggleTun(enable bool) {
	if enable && u.closePending {
		return
	}

	if !isProcessElevated() {
		u.d.storeTun(enable)
		flags := appFlags(1<<afTun | 1<<afRestart)
		if launchSelf(flagsToCmdLine(flags), u.hwnd, true) {
			u.requestClose()
		}
		return
	}

	// Turning TUN on only takes effect after the core restarts; the tray is
	// updated from the running state instead of the request.
	u.toggleTunDisplay(enable, !enable)
	u.tunBusy = true
	u.d.storeTun(enable)
	u.d.startCore(enable)
}

func (u *mainUI) editSelector(action selectorAction) {
	u.lastRequestID++
	requestID := u.lastRequestID

	if u.d.editSelector(action.SelectorIndex, action.OutboundIndex, requestID) {
		u.pending[requestID] = action
	}
}

func (u *mainUI) selectMode(mode string) {
	if strings.EqualFold(mode, u.d.clashMode) {
		return
	}
	u.d.setClashMode(mode)
}

func (u *mainUI) handleDroverEvent(event droverEvent) {
	switch event.Kind {
	case dekError:
		u.tunBusy = false
		u.updateTrayIcon()
		u.showBalloon(event.Msg, "", niifError, 10000)

	case dekRunning:
		u.tunBusy = false
		u.toggleTunDisplay(u.d.isTunActive, true)

	case dekClashModes:
		// The mode submenu is rebuilt on every open.

	case dekCoreEvent:
		if event.CoreEvent.Kind == cekSelectorDone {
			delete(u.pending, event.CoreEvent.RequestID)
		}
	}
}

// --- Autostart -----------------------------------------------------------

func (u *mainUI) initAutostart() {
	u.autostartState = asUnknown

	action := aaCheck
	if u.startupFlags.has(afAutostartEnable) {
		action = aaEnable
	} else if u.startupFlags.has(afAutostartDisable) {
		action = aaDisable
	}
	u.startAutostart(action)
}

func (u *mainUI) startAutostart(action autostartAction) {
	u.autostartBusy = true
	runAutostart(action, func(result autostartResult) {
		u.post(func() { u.applyAutostartResult(result) })
	})
}

func (u *mainUI) applyAutostartResult(result autostartResult) {
	u.autostartBusy = false
	u.autostartState = result.State

	if !result.Success && result.Action != aaCheck {
		u.showBalloon(result.ErrorMsg, "", niifError, 10000)
	}
}

func (u *mainUI) toggleAutostart() {
	if u.closePending {
		return
	}

	action := aaEnable
	flag := appFlag(afAutostartEnable)
	if u.autostartState == asEnabled {
		action = aaDisable
		flag = afAutostartDisable
	}

	if !isProcessElevated() {
		flags := appFlags(1<<afRestart | 1<<flag)
		if launchSelf(flagsToCmdLine(flags), u.hwnd, true) {
			u.requestClose()
		}
		return
	}

	u.startAutostart(action)
}

// --- Settings window -----------------------------------------------------

func (u *mainUI) showSettings() {
	if u.d == nil || u.closePending {
		return
	}
	if u.settingsClosed && getTickCount()-u.settingsClosedTick < 400 {
		return
	}

	if u.settings != nil {
		u.settings.showAgain()
		return
	}
	if u.settingsOpening {
		return
	}

	u.settingsOpening = true
	defer func() { u.settingsOpening = false }()

	settings, err := newSettingsDialog(u.hwnd, false, "")
	if err != nil {
		u.showBalloon(err.Error(), "", niifError, 10000)
		return
	}

	u.settings = settings
	settings.onClosed = u.handleSettingsClosed
	settings.show()
}

func (u *mainUI) handleSettingsClosed(settings *settingsDialog) {
	if u.settings == settings {
		u.settings = nil
	}
	u.settingsClosed = true
	u.settingsClosedTick = getTickCount()

	if u.closePending || u.d == nil {
		return
	}
	if settings.result != idOK {
		return
	}
	if settings.restartRequired {
		u.restartApp()
		return
	}
	u.d.setSelectorMenuLayout(settings.selectedLayout)
}

func (u *mainUI) closeSettings() {
	settings := u.settings
	if settings == nil {
		return
	}
	u.settings = nil
	settings.onClosed = nil
	settings.destroy()
}

// --- Shell helpers -------------------------------------------------------

func shellOpen(target string) bool {
	if target == "" {
		return false
	}

	var sei shellExecuteInfo
	sei.CbSize = uint32(unsafe.Sizeof(sei))
	sei.FMask = seeMaskNoAsync
	sei.LpVerb = utf16Ptr("open")
	sei.LpFile = utf16Ptr(target)
	sei.NShow = swNormal
	r, _, _ := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	runtime.KeepAlive(&sei)
	return r != 0
}
