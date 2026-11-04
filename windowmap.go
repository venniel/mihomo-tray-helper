package main

import "sync"

var (
	windowMapMu     sync.Mutex
	mainWindows     = map[uintptr]*mainUI{}
	settingsWindows = map[uintptr]*settingsDialog{}
)

func registerMainWindow(hwnd uintptr, ui *mainUI) {
	windowMapMu.Lock()
	mainWindows[hwnd] = ui
	windowMapMu.Unlock()
}

func unregisterMainWindow(hwnd uintptr) {
	windowMapMu.Lock()
	delete(mainWindows, hwnd)
	windowMapMu.Unlock()
}

func lookupMainWindow(hwnd uintptr) *mainUI {
	windowMapMu.Lock()
	ui := mainWindows[hwnd]
	windowMapMu.Unlock()
	return ui
}

func registerSettingsWindow(hwnd uintptr, dialog *settingsDialog) {
	windowMapMu.Lock()
	settingsWindows[hwnd] = dialog
	windowMapMu.Unlock()
}

func unregisterSettingsWindow(hwnd uintptr) {
	windowMapMu.Lock()
	delete(settingsWindows, hwnd)
	windowMapMu.Unlock()
}

func lookupSettingsWindow(hwnd uintptr) *settingsDialog {
	windowMapMu.Lock()
	dialog := settingsWindows[hwnd]
	windowMapMu.Unlock()
	return dialog
}
