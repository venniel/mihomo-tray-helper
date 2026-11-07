package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	mainWindowClass = "Nexil-main"
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

	hIconEnabled   uintptr
	hIconDisabled  uintptr
	hIconTun       uintptr
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
	u.hIconEnabled = loadEmbeddedIcon(iconEnabledData, smallX, smallY)
	u.hIconDisabled = loadEmbeddedIcon(iconDisabledData, smallX, smallY)
	u.hIconTun = loadEmbeddedIcon(iconTunData, smallX, smallY)
	if u.hIconEnabled == 0 {
		u.hIconEnabled = loadApplicationIcon(smallX, smallY)
	}
	if u.hIconDisabled == 0 {
		u.hIconDisabled = u.hIconEnabled
	}
	if u.hIconTun == 0 {
		u.hIconTun = u.hIconEnabled
	}

	wc := wndClassEx{
		Style:         0,
		LpfnWndProc:   mainWindowProc,
		HInstance:     hInstance,
		HIcon:         u.hIconLarge,
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: utf16Ptr(mainWindowClass),
		HIconSm:       u.hIconEnabled,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))

	atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(&wc)
	if atom == 0 {
		if err := windows.GetLastError(); err != windows.ERROR_CLASS_ALREADY_EXISTS {
			return nil, fmt.Errorf("Failed to register main window class: %s", err.Error())
		}
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

	case wmCommand:
		u.handleCommand(loword(wparam), hiword(wparam))
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
	for _, icon := range []uintptr{u.hIconEnabled, u.hIconDisabled, u.hIconTun, u.hIconLarge} {
		if icon != 0 {
			pDestroyIcon.Call(icon)
		}
	}
	u.hIconEnabled = 0
	u.hIconDisabled = 0
	u.hIconTun = 0
	u.hIconLarge = 0
}

// --- Tray icon -----------------------------------------------------------

func (u *mainUI) currentTrayIcon() uintptr {
	if u.isTunActive {
		return u.hIconTun
	}
	if u.isSystemProxyActive {
		return u.hIconEnabled
	}
	return u.hIconDisabled
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
	nid.HIcon = u.currentTrayIcon()
	copyToUTF16Array(nid.SzTip[:], appTitle)

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
	nid.UFlags = nifIcon | nifShowTip
	nid.HIcon = u.currentTrayIcon()
	copyToUTF16Array(nid.SzTip[:], appTitle)
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

// --- Menu ----------------------------------------------------------------

func (u *mainUI) showTrayMenu() {
	u.d.refreshSelectorSnapshot()

	hmenu, _ := u.buildMenu()
	if hmenu == 0 {
		return
	}

	pt, _ := getCursorPos()
	pSetForegroundWindow.Call(u.hwnd)
	cmd, _, _ := pTrackPopupMenuEx.Call(hmenu,
		tpmRightBtn|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y),
		u.hwnd, 0)
	pDestroyMenu.Call(hmenu)
	postMessage(u.hwnd, wmNull, 0, 0)

	if cmd != 0 {
		u.executeMenuCommand(uint32(cmd))
	}
}

func (u *mainUI) buildMenu() (uintptr, bool) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return 0, false
	}

	u.menuActions = map[uint32]selectorAction{}
	u.menuModes = nil

	if u.closePending {
		appendMenu(menu, mfString, idMenuQuit, "退出")
		return menu, true
	}

	d := u.d

	extras, _, _ := pCreatePopupMenu.Call()
	appendMenu(extras, mfString, idMenuRestart, "重启")

	autostartFlags := uintptr(mfString)
	if u.autostartBusy || u.autostartState == asUnknown {
		autostartFlags |= mfGrayed
	}
	if u.autostartState == asEnabled {
		autostartFlags |= mfChecked
	}
	appendMenu(extras, autostartFlags, idMenuAutostart, "开机自启")
	appendMenu(extras, mfString, idMenuHomepage, "项目主页")
	appendSubMenu(menu, extras, "更多")

	u.appendModeMenu(menu)
	u.appendNodeMenu(menu)

	if d.mihomoConfig.HasTun {
		flags := uintptr(mfString)
		if u.isTunActive {
			flags |= mfChecked
		}
		if u.tunBusy {
			flags |= mfGrayed
		}
		appendMenu(menu, flags, idMenuTun, "TUN 模式")
	}

	proxyFlags := uintptr(mfString)
	if u.isSystemProxyActive {
		proxyFlags |= mfChecked
	}
	appendMenu(menu, proxyFlags, idMenuSystemProxy, "系统代理")

	if d.mihomoConfig.ClashAPI.DashboardURL() != "" {
		appendMenu(menu, mfString, idMenuDashboard, "面板")
	}

	appendMenu(menu, mfString, idMenuQuit, "退出")
	return menu, true
}

func (u *mainUI) appendModeMenu(menu uintptr) {
	modes := u.d.clashModes
	if len(modes) == 0 {
		return
	}
	if len(modes) >= idMenuSelectorBase-idMenuModeBase {
		modes = modes[:idMenuSelectorBase-idMenuModeBase]
	}
	u.menuModes = append([]string(nil), modes...)

	sub, _, _ := pCreatePopupMenu.Call()
	if sub == 0 {
		return
	}
	for i, mode := range modes {
		flags := uintptr(mfString)
		if strings.EqualFold(mode, u.d.clashMode) {
			flags |= mfChecked
		}
		appendMenu(sub, flags, uintptr(idMenuModeBase+i), mode)
	}
	appendSubMenu(menu, sub, "模式")
}

func (u *mainUI) appendNodeMenu(menu uintptr) {
	type nodeGroup struct {
		index    int
		selector configSelector
	}

	groups := make([]nodeGroup, 0, len(u.d.selectors))
	for i, selector := range u.d.selectors {
		if len(selector.Outbounds) == 0 {
			continue
		}
		groups = append(groups, nodeGroup{index: i, selector: selector})
	}

	count := len(groups)
	if count < 1 || count > 50 {
		return
	}

	isNested := false
	switch u.d.options.SelectorMenuLayout {
	case smlFlat:
		isNested = false
	case smlNested:
		isNested = true
	default:
		flatRows := 2 * count
		for _, group := range groups {
			flatRows += len(group.selector.Outbounds)
		}
		isNested = flatRows > 20
	}

	disabled := map[int]bool{}
	for _, action := range u.pending {
		disabled[action.SelectorIndex*1000+action.OutboundIndex] = true
	}

	sub, _, _ := pCreatePopupMenu.Call()
	if sub == 0 {
		return
	}

	nextID := idMenuSelectorBase
	for position, group := range groups {
		selectorIdx := group.index
		selector := group.selector

		var selectorMenu uintptr
		if isNested {
			selectorMenu, _, _ = pCreatePopupMenu.Call()
			if selectorMenu == 0 {
				continue
			}
		} else {
			appendMenu(sub, mfString|mfDisabled, 0, selector.Name)
		}

		checkedIndex := selector.DefaultIndex
		if now, ok := u.d.selectorNow(selector.Name); ok {
			for j, outbound := range selector.Outbounds {
				if outbound == now {
					checkedIndex = j
					break
				}
			}
		}

		for outboundIdx, outbound := range selector.Outbounds {
			flags := uintptr(mfString)
			if outboundIdx == checkedIndex {
				flags |= mfChecked
			}
			if disabled[selectorIdx*1000+outboundIdx] {
				flags |= mfGrayed
			}
			id := uint32(nextID)
			nextID++
			u.menuActions[id] = selectorAction{SelectorIndex: selectorIdx, OutboundIndex: outboundIdx}
			if isNested {
				appendMenu(selectorMenu, flags, uintptr(id), outbound)
			} else {
				appendMenu(sub, flags, uintptr(id), outbound)
			}
		}

		if isNested {
			appendSubMenu(sub, selectorMenu, selector.Name)
		} else if position < count-1 {
			appendMenu(sub, mfSeparator, 0, "")
		}
	}

	appendSubMenu(menu, sub, "节点")
}

func (u *mainUI) handleCommand(id, notification uint32) {
	switch id {
	case idMenuRestart:
		if notification == bnClicked {
			u.restartApp()
		}
	case idMenuAutostart:
		if notification == bnClicked {
			u.toggleAutostart()
		}
	case idMenuHomepage:
		shellOpen(homepageURL)
	case idMenuDashboard:
		if url := u.d.mihomoConfig.ClashAPI.DashboardURL(); url != "" {
			shellOpen(url)
		}
	case idMenuQuit:
		if notification == bnClicked {
			u.requestClose()
		}
	case idMenuTun:
		if notification == bnClicked {
			u.toggleTun(!u.isTunActive)
		}
	case idMenuSystemProxy:
		if notification == bnClicked {
			u.toggleSystemProxy(!u.isSystemProxyActive, true, true)
		}
	}
}

func (u *mainUI) executeMenuCommand(cmd uint32) {
	switch cmd {
	case idMenuRestart:
		u.restartApp()
		return
	case idMenuAutostart:
		u.toggleAutostart()
		return
	case idMenuHomepage:
		shellOpen(homepageURL)
		return
	case idMenuDashboard:
		if url := u.d.mihomoConfig.ClashAPI.DashboardURL(); url != "" {
			shellOpen(url)
		}
		return
	case idMenuQuit:
		u.requestClose()
		return
	case idMenuTun:
		u.toggleTun(!u.isTunActive)
		return
	case idMenuSystemProxy:
		u.toggleSystemProxy(!u.isSystemProxyActive, true, true)
		return
	}

	if action, ok := u.menuActions[cmd]; ok {
		u.editSelector(action)
		return
	}
	if cmd >= idMenuModeBase && int(cmd) < idMenuModeBase+len(u.menuModes) {
		u.selectMode(u.menuModes[cmd-idMenuModeBase])
	}
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

	u.isSystemProxyActive = enable
	if enable {
		u.d.enableSystemProxy()
	} else {
		u.d.disableSystemProxy()
	}
	if persist {
		u.d.storeSystemProxy(enable)
	}
	if updateIcon {
		u.updateTrayIcon()
	}
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

	u.toggleTunDisplay(enable, true)
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
