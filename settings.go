package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	settingsWindowClass = "Nexil-settings"

	idSettingsCoreEdit     = 1001
	idSettingsBrowseCore   = 1002
	idSettingsConfigEdit   = 1003
	idSettingsBrowseConfig = 1004
	idSettingsLayout       = 1005
	idSettingsError        = 1006
	idSettingsSave         = idOK
	idSettingsCancel       = idCancel
)

type settingsDialog struct {
	hwnd  uintptr
	dpi   uint32
	fonts []uintptr

	hCoreLabel    uintptr
	hCoreEdit     uintptr
	hBrowseCore   uintptr
	hConfigLabel  uintptr
	hConfigEdit   uintptr
	hBrowseConfig uintptr
	hLayoutLabel  uintptr
	hLayoutCombo  uintptr
	hErrorLabel   uintptr
	hSave         uintptr
	hCancelButton uintptr

	optionsFile       string
	initialCoreFile   string
	initialConfigFile string
	selectedLayout    selectorMenuLayout
	restartRequired   bool
	hasError          bool
	errorText         string
	result            int
	finished          bool
	onClosed          func(*settingsDialog)
}

var settingsWindowProc = windows.NewCallback(settingsWndProcFunc)

var (
	settingsClassOnce sync.Once
	settingsClassErr  error
)

func settingsWndProcFunc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if settings := lookupSettingsWindow(hwnd); settings != nil {
		return settings.handleMessage(hwnd, msg, wparam, lparam)
	}

	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func ensureSettingsClass() error {
	settingsClassOnce.Do(func() {
		hInstance := getModuleHandle()
		cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
		icon := loadApplicationIcon(getSystemMetric(smCXIcon), getSystemMetric(smCYIcon))
		wc := wndClassEx{
			LpfnWndProc:   settingsWindowProc,
			HInstance:     hInstance,
			HIcon:         icon,
			HCursor:       cursor,
			HbrBackground: colorBtnFace + 1,
			LpszClassName: utf16Ptr(settingsWindowClass),
			HIconSm:       icon,
		}
		wc.CbSize = uint32(unsafe.Sizeof(wc))

		atom, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		runtime.KeepAlive(&wc)
		if atom == 0 {
			if err := windows.GetLastError(); err != windows.ERROR_CLASS_ALREADY_EXISTS {
				settingsClassErr = fmt.Errorf("Failed to register settings window class: %s", err.Error())
			}
		}
	})
	return settingsClassErr
}

func newSettingsDialog(owner uintptr, firstRun bool, errorMessage string) (*settingsDialog, error) {
	if err := ensureSettingsClass(); err != nil {
		return nil, err
	}

	optionsFile := programDir() + optionsFilename
	opts := loadOptions(optionsFile)

	s := &settingsDialog{
		optionsFile:    optionsFile,
		result:         idCancel,
		selectedLayout: opts.SelectorMenuLayout,
	}

	s.initialCoreFile = filepath.Clean(opts.CoreFile)
	s.initialConfigFile = filepath.Clean(opts.ConfigFile)

	caption := "Nexil 设置"
	if firstRun {
		caption = "Nexil 初始设置"
	}

	s.dpi = getSystemDPI()
	style := uintptr(wsPopup | wsCaption | wsSysMenu | wsClipChildren)
	exStyle := uintptr(wsExDlgModalFrm | wsExAppWindow | wsExCtrlParent)

	clientWidth := scaleValue(500, s.dpi)
	clientHeight := scaleValue(260, s.dpi)
	rect := winRect{Left: 0, Top: 0, Right: clientWidth, Bottom: clientHeight}
	adjustWindowRectEx(&rect, style, exStyle, false)
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top

	var work winRect
	pSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	x := work.Left + (work.Right-work.Left-width)/2
	y := work.Top + (work.Bottom-work.Top-height)/2

	hwnd := createWindowEx(exStyle, settingsWindowClass, caption, style,
		x, y, width, height, owner, 0, getModuleHandle(), 0)
	if hwnd == 0 {
		return nil, errors.New("Failed to create settings window.")
	}
	s.hwnd = hwnd
	registerSettingsWindow(hwnd, s)

	s.createControls()
	setWindowText(s.hCoreEdit, opts.CoreFile)
	setWindowText(s.hConfigEdit, opts.ConfigFile)

	s.hLayoutCombo = getDlgItem(hwnd, idSettingsLayout)
	sendMessage(s.hLayoutCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr("自动"))))
	sendMessage(s.hLayoutCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr("扁平"))))
	sendMessage(s.hLayoutCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr("嵌套"))))

	index := 0
	switch opts.SelectorMenuLayout {
	case smlFlat:
		index = 1
	case smlNested:
		index = 2
	}
	sendMessage(s.hLayoutCombo, cbSetCurSel, uintptr(index), 0)

	if errorMessage != "" {
		s.setError(errorMessage)
	}

	return s, nil
}

// Child windows are z-ordered in creation order, and dialog tab navigation
// follows that order, so controls must be created in visual reading order.
func (s *settingsDialog) createControls() {
	s.hCoreLabel = s.createControl("STATIC", "代理核心文件", ssLeft, 0, 0)
	s.hCoreEdit = s.createControl("EDIT", "", wsBorder|esAutoHScroll|wsTabStop, 0, idSettingsCoreEdit)
	s.hBrowseCore = s.createControl("BUTTON", "浏览...", bsPushButton|wsTabStop, 0, idSettingsBrowseCore)

	s.hConfigLabel = s.createControl("STATIC", "配置文件", ssLeft, 0, 0)
	s.hConfigEdit = s.createControl("EDIT", "", wsBorder|esAutoHScroll|wsTabStop, 0, idSettingsConfigEdit)
	s.hBrowseConfig = s.createControl("BUTTON", "浏览...", bsPushButton|wsTabStop, 0, idSettingsBrowseConfig)

	s.hLayoutLabel = s.createControl("STATIC", "节点菜单样式", ssLeft, 0, 0)
	s.hLayoutCombo = s.createControl("COMBOBOX", "", cbsDropDownList|wsTabStop|wsVScroll, 0, idSettingsLayout)

	s.hErrorLabel = s.createControl("STATIC", "", ssLeft, 0, idSettingsError)
	showWindow(s.hErrorLabel, swHide)

	s.hSave = s.createControl("BUTTON", "保存", bsDefPushButton|wsTabStop, 0, idSettingsSave)
	s.hCancelButton = s.createControl("BUTTON", "取消", bsPushButton|wsTabStop, 0, idSettingsCancel)

	setAccessibleName(s.hCoreEdit, "代理核心文件")
	setAccessibleName(s.hConfigEdit, "配置文件")
	setAccessibleName(s.hLayoutCombo, "节点菜单样式")
	setAccessibleName(s.hBrowseCore, "浏览代理核心文件")
	setAccessibleName(s.hBrowseConfig, "浏览配置文件")

	s.layoutControls()
}

func (s *settingsDialog) createControl(className, text string, style, exStyle uintptr, id int) uintptr {
	return createWindowEx(exStyle, className, text,
		style|wsChild|wsVisible,
		0, 0, 0, 0,
		s.hwnd, uintptr(id), getModuleHandle(), 0)
}

func (s *settingsDialog) currentFont() uintptr {
	if len(s.fonts) == 0 {
		return 0
	}
	return s.fonts[len(s.fonts)-1]
}

func (s *settingsDialog) layoutControls() {
	font := createUiFont(s.dpi)
	s.fonts = append(s.fonts, font)

	for _, hwnd := range []uintptr{
		s.hCoreLabel, s.hCoreEdit, s.hBrowseCore,
		s.hConfigLabel, s.hConfigEdit, s.hBrowseConfig,
		s.hLayoutLabel, s.hLayoutCombo,
		s.hErrorLabel, s.hSave, s.hCancelButton,
	} {
		if hwnd != 0 {
			setWindowFont(hwnd, font)
		}
	}

	s.place(s.hCoreLabel, 12, 14, 103, 19)
	s.place(s.hCoreEdit, 12, 35, 385, 23)
	s.place(s.hBrowseCore, 405, 33, 83, 25)
	s.place(s.hConfigLabel, 12, 67, 52, 19)
	s.place(s.hConfigEdit, 12, 88, 385, 23)
	s.place(s.hBrowseConfig, 405, 86, 83, 25)
	s.place(s.hLayoutLabel, 12, 120, 78, 19)
	s.place(s.hLayoutCombo, 12, 141, 160, 200)
	s.place(s.hErrorLabel, 12, 172, 476, 32)
	s.place(s.hSave, 316, 222, 83, 27)
	s.place(s.hCancelButton, 405, 222, 83, 27)
}

func (s *settingsDialog) place(hwnd uintptr, x, y, w, h int) {
	if hwnd == 0 {
		return
	}
	setWindowPos(hwnd, 0,
		scaleValue(x, s.dpi), scaleValue(y, s.dpi),
		scaleValue(w, s.dpi), scaleValue(h, s.dpi),
		swpNoZOrder|swpNoActivate)
}

func (s *settingsDialog) handleMessage(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch uint32(msg) {
	case wmCommand:
		s.handleCommand(int(loword(wparam)), hiword(wparam))
		return 0

	case dmGetDefID:
		return uintptr(uint32(dcHasDefID)<<16 | uint32(idOK))

	case wmCtlColorStatic:
		pSetBkMode.Call(wparam, bkmodeTransparent)
		color, _, _ := pGetSysColor.Call(colorWindowText)
		pSetTextColor.Call(wparam, color)
		brush, _, _ := pGetSysColorBrush.Call(colorBtnFace)
		return brush

	case wmDpiChanged:
		s.applyDpi(uint32(loword(wparam)))
		return 0

	case wmClose:
		s.finish(idCancel)
		return 0

	case wmNcDestroy:
		unregisterSettingsWindow(hwnd)
		s.cleanup()
		if s.onClosed != nil {
			callback := s.onClosed
			s.onClosed = nil
			callback(s)
		}
	}

	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (s *settingsDialog) handleCommand(id int, notification uint32) {
	switch id {
	case idSettingsBrowseCore:
		if notification == bnClicked {
			s.browseCore()
		}
	case idSettingsBrowseConfig:
		if notification == bnClicked {
			s.browseConfig()
		}
	case idSettingsSave:
		s.save()
	case idSettingsCancel:
		s.finish(idCancel)
	}
}

func (s *settingsDialog) applyDpi(dpi uint32) {
	if dpi == 0 {
		dpi = 96
	}
	if dpi == s.dpi {
		return
	}
	s.dpi = dpi
	s.layoutControls()

	style := uintptr(wsPopup | wsCaption | wsSysMenu | wsClipChildren)
	exStyle := uintptr(wsExDlgModalFrm | wsExAppWindow | wsExCtrlParent)
	rect := winRect{Right: scaleValue(500, dpi), Bottom: scaleValue(260, dpi)}
	adjustWindowRectEx(&rect, style, exStyle, false)
	setWindowPos(s.hwnd, 0, 0, 0, rect.Right-rect.Left, rect.Bottom-rect.Top,
		swpNoMove|swpNoZOrder|swpNoActivate)
}

func (s *settingsDialog) cleanup() {
	for _, font := range s.fonts {
		if font != 0 {
			pDeleteObject.Call(font)
		}
	}
	s.fonts = nil
}

func (s *settingsDialog) show() {
	showWindow(s.hwnd, swShow)
	pSetForegroundWindow.Call(s.hwnd)
	setFocus(s.hCoreEdit)
	if s.hasError {
		notifyWinEvent(eventSystemAlert, s.hErrorLabel)
		s.announceError()
	}
}

func (s *settingsDialog) showAgain() {
	if r, _, _ := pIsWindow.Call(s.hwnd); r == 0 {
		return
	}
	showWindow(s.hwnd, swShow)
	pBringWindowToTop.Call(s.hwnd)
	pSetForegroundWindow.Call(s.hwnd)

	focus := getFocus()
	if focus == 0 || !isChild(s.hwnd, focus) {
		setFocus(s.hCoreEdit)
	}
}

func (s *settingsDialog) runModal() {
	if s.hwnd == 0 {
		return
	}

	var msg winMsg
	for {
		if r, _, _ := pIsWindow.Call(s.hwnd); r == 0 {
			break
		}
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if r2, _, _ := pIsDialogMessageW.Call(s.hwnd, uintptr(unsafe.Pointer(&msg))); r2 != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (s *settingsDialog) destroy() {
	if s.hwnd != 0 {
		pDestroyWindow.Call(s.hwnd)
	}
}

func (s *settingsDialog) finish(result int) {
	if s.finished {
		return
	}
	s.finished = true
	s.result = result
	pDestroyWindow.Call(s.hwnd)
}

func (s *settingsDialog) setError(message string) {
	s.hasError = true
	s.errorText = message
	setWindowText(s.hErrorLabel, message)
	showWindow(s.hErrorLabel, swShow)
	notifyWinEvent(eventSystemAlert, s.hErrorLabel)
	notifyWinEvent(eventObjectShow, s.hErrorLabel)
}

func (s *settingsDialog) announceError() {
	messageBoxFor(s.hwnd, s.errorText, appTitle, mbIconError|mbOK)
}

func (s *settingsDialog) showError(message string) {
	s.setError(message)
	s.announceError()
}

func (s *settingsDialog) browseCore() {
	current := cleanPath(getWindowText(s.hCoreEdit))
	initial := ""
	var initialDir *uint16
	if fileExists(current) {
		initial = current
		initialDir = utf16Ptr(filepath.Dir(current))
	}

	buffer := make([]uint16, 32768)
	copyToUTF16Array(buffer, initial)

	filter := utf16FilterString("核心程序 (*.exe)\x00*.exe\x00所有文件 (*.*)\x00*.*\x00")

	ofn := openFileName{
		LStructSize:     uint32(unsafe.Sizeof(openFileName{})),
		HwndOwner:       s.hwnd,
		LpstrFilter:     &filter[0],
		LpstrFile:       &buffer[0],
		NMaxFile:        uint32(len(buffer)),
		LpstrInitialDir: initialDir,
		LpstrTitle:      utf16Ptr("选择代理核心文件"),
		NFilterIndex:    1,
		Flags:           ofnFileMustExist | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}

	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r != 0 {
		setWindowText(s.hCoreEdit, windows.UTF16ToString(buffer))
	}
	runtime.KeepAlive(&ofn)
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(filter)
}

func (s *settingsDialog) browseConfig() {
	current := cleanPath(getWindowText(s.hConfigEdit))
	initial := ""
	var initialDir *uint16
	if fileExists(current) {
		initial = current
		initialDir = utf16Ptr(filepath.Dir(current))
	} else if coreFile := cleanPath(getWindowText(s.hCoreEdit)); fileExists(coreFile) {
		initialDir = utf16Ptr(filepath.Dir(coreFile))
	}

	buffer := make([]uint16, 32768)
	copyToUTF16Array(buffer, initial)

	filter := utf16FilterString("配置文件 (*.yaml;*.yml)\x00*.yaml;*.yml\x00所有文件 (*.*)\x00*.*\x00")

	ofn := openFileName{
		LStructSize:     uint32(unsafe.Sizeof(openFileName{})),
		HwndOwner:       s.hwnd,
		LpstrFilter:     &filter[0],
		LpstrFile:       &buffer[0],
		NMaxFile:        uint32(len(buffer)),
		LpstrInitialDir: initialDir,
		LpstrTitle:      utf16Ptr("选择配置文件"),
		NFilterIndex:    1,
		Flags:           ofnFileMustExist | ofnPathMustExist | ofnExplorer | ofnNoChangeDir,
	}

	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r != 0 {
		setWindowText(s.hConfigEdit, windows.UTF16ToString(buffer))
	}
	runtime.KeepAlive(&ofn)
	runtime.KeepAlive(buffer)
	runtime.KeepAlive(filter)
}

func (s *settingsDialog) save() {
	coreFile := absolutePath(getWindowText(s.hCoreEdit))
	if coreFile == "" {
		s.showError("请选择代理核心文件。")
		setFocus(s.hCoreEdit)
		return
	}
	if !fileExists(coreFile) {
		s.showError("找不到代理核心文件 " + coreFile + "。")
		setFocus(s.hCoreEdit)
		return
	}

	configFile := absolutePath(getWindowText(s.hConfigEdit))
	if configFile == "" {
		s.showError("请选择配置文件。")
		setFocus(s.hConfigEdit)
		return
	}
	if !fileExists(configFile) {
		s.showError("找不到配置文件 " + configFile + "。")
		setFocus(s.hConfigEdit)
		return
	}

	layout := smlAuto
	switch int(sendMessage(s.hLayoutCombo, cbGetCurSel, 0, 0)) {
	case 1:
		layout = smlFlat
	case 2:
		layout = smlNested
	}

	if err := saveOptions(s.optionsFile, coreFile, configFile, layout); err != nil {
		s.showError("保存设置失败：" + err.Error())
		return
	}

	s.selectedLayout = layout
	s.restartRequired = !strings.EqualFold(coreFile, s.initialCoreFile) ||
		!strings.EqualFold(configFile, s.initialConfigFile)
	s.finish(idOK)
}

func cleanPath(value string) string {
	path := strings.TrimSpace(value)
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		path = strings.TrimSpace(path[1 : len(path)-1])
	}
	return path
}

func absolutePath(value string) string {
	path := cleanPath(value)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(programDir(), path)
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(full)
}

func showSettingsModal(firstRun bool, errorMessage string) int {
	settings, err := newSettingsDialog(0, firstRun, errorMessage)
	if err != nil {
		messageBox(err.Error(), appTitle, mbIconError)
		return idCancel
	}

	settings.show()
	settings.runModal()
	return settings.result
}

func createUiFont(dpi uint32) uintptr {
	height := -int32((int64(9)*int64(dpi) + 36) / 72)
	font, _, _ := pCreateFontW.Call(
		uintptr(height),
		0, 0, 0,
		fwNormal,
		0, 0, 0,
		defaultCharset,
		0, 0,
		cleartypeQuality,
		defaultPitch,
		uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))))
	return font
}

func scaleValue(value int, dpi uint32) int32 {
	return int32((int64(value)*int64(dpi) + 48) / 96)
}

func getSystemDPI() uint32 {
	if r, _, _ := pGetDpiForSystem.Call(); r != 0 {
		return uint32(r)
	}
	return 96
}
