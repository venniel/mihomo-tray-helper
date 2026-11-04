package main

import (
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	elevationOnce sync.Once
	elevationVal  bool
)

func isProcessElevated() bool {
	elevationOnce.Do(func() {
		var token windows.Token
		if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
			return
		}
		defer token.Close()
		elevationVal = token.IsElevated()
	})
	return elevationVal
}

func currentExecutablePath() string {
	path, err := os.Executable()
	if err != nil || path == "" {
		return os.Args[0]
	}
	return path
}

func launchSelf(params string, ownerHwnd uintptr, elevate bool) bool {
	verb := "open"
	if elevate {
		verb = "runas"
	}

	exe := currentExecutablePath()
	var sei shellExecuteInfo
	sei.CbSize = uint32(unsafe.Sizeof(sei))
	sei.FMask = seeMaskNoAsync
	sei.Hwnd = ownerHwnd
	sei.LpVerb = utf16Ptr(verb)
	sei.LpFile = utf16Ptr(exe)
	sei.LpParameters = utf16Ptr(params)
	sei.LpDirectory = utf16Ptr(filepath.Dir(exe))
	sei.NShow = swNormal

	r, _, _ := pShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	return r != 0
}
