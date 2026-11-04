package main

import (
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	internetPerConnFlags          = 1
	internetPerConnProxyServer    = 2
	internetPerConnProxyBypass    = 3
	proxyTypeDirect               = 0x00000001
	proxyTypeProxy                = 0x00000002
	internetOptionRefresh         = 37
	internetOptionPerConnOption   = 75
	internetOptionSettingsChanged = 39
)

var (
	wininet             = windows.NewLazySystemDLL("wininet.dll")
	pInternetSetOptionW = wininet.NewProc("InternetSetOptionW")
)

func setProxyOptions(proxyStr string) bool {
	var list internetPerConnOptionList
	var opts [3]internetPerConnOption

	list.DwSize = uint32(unsafe.Sizeof(list))
	list.POptions = &opts[0]

	var proxyPtr, bypassPtr *uint16

	if proxyStr == "" {
		list.DwOptionCount = 1
		opts[0].DwOption = internetPerConnFlags
		opts[0].Value = proxyTypeDirect
	} else {
		list.DwOptionCount = 3

		opts[0].DwOption = internetPerConnFlags
		opts[0].Value = proxyTypeDirect | proxyTypeProxy

		proxyPtr = utf16Ptr(proxyStr)
		bypassPtr = utf16Ptr("<local>")

		opts[1].DwOption = internetPerConnProxyServer
		opts[1].Value = uintptr(unsafe.Pointer(proxyPtr))

		opts[2].DwOption = internetPerConnProxyBypass
		opts[2].Value = uintptr(unsafe.Pointer(bypassPtr))
	}

	r, _, _ := pInternetSetOptionW.Call(0, internetOptionPerConnOption,
		uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list))
	runtime.KeepAlive(proxyPtr)
	runtime.KeepAlive(bypassPtr)
	if r == 0 {
		return false
	}

	pInternetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	pInternetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
	return true
}

func enableSystemProxy(host string, port int, socksOnly bool) bool {
	value := host + ":" + strconv.Itoa(port)
	if socksOnly {
		return setProxyOptions("socks=" + value)
	}
	proxyStr := "http=" + value + ";https=" + value + ";socks=" + value
	return setProxyOptions(proxyStr)
}

func disableSystemProxy() bool {
	return setProxyOptions("")
}
