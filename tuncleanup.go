package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	digcfAllClasses      = 0x00000004
	difRemove            = 0x00000005
	diRemoveDeviceGlobal = 0x00000001
	dnStarted            = 0x00000008
	crSuccess            = 0
	wintunDevicePrefix   = `SWD\WINTUN\`
)

func getDeviceInstanceID(deviceInfoSet uintptr, deviceInfo *spDevInfoData) string {
	buffer := make([]uint16, 1024)
	var required uint32
	r, _, _ := pSetupDiGetDeviceInstanceIdW.Call(deviceInfoSet, uintptr(unsafe.Pointer(deviceInfo)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&required)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

func isDeviceStarted(deviceInfo *spDevInfoData) bool {
	var status, problem uint32
	r, _, _ := pCMGetDevNodeStatus.Call(uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&problem)),
		uintptr(deviceInfo.DevInst), 0)
	return uint32(r) == crSuccess && status&dnStarted != 0
}

func removeDevice(deviceInfoSet uintptr, deviceInfo *spDevInfoData) bool {
	var params spRemoveDeviceParams
	params.ClassInstallHeader.CbSize = uint32(unsafe.Sizeof(spClassInstallHeader{}))
	params.ClassInstallHeader.InstallFunction = difRemove
	params.Scope = diRemoveDeviceGlobal

	r, _, _ := pSetupDiSetClassInstallParamsW.Call(deviceInfoSet, uintptr(unsafe.Pointer(deviceInfo)),
		uintptr(unsafe.Pointer(&params)), unsafe.Sizeof(params))
	if r == 0 {
		return false
	}
	r, _, _ = pSetupDiCallClassInstaller.Call(difRemove, deviceInfoSet, uintptr(unsafe.Pointer(deviceInfo)))
	return r != 0
}

func removeStaleWintunAdapters() int {
	removedCount := 0

	for pass := 0; pass < 16; pass++ {
		deviceInfoSet, _, _ := pSetupDiGetClassDevsW.Call(0, 0, 0, digcfAllClasses)
		if deviceInfoSet == 0 || deviceInfoSet == ^uintptr(0) {
			return removedCount
		}

		removedInPass := false
		index := uint32(0)
		for {
			var deviceInfo spDevInfoData
			deviceInfo.CbSize = uint32(unsafe.Sizeof(deviceInfo))

			ok, _, _ := pSetupDiEnumDeviceInfo.Call(deviceInfoSet, uintptr(index), uintptr(unsafe.Pointer(&deviceInfo)))
			if ok == 0 {
				break
			}
			index++

			instanceID := getDeviceInstanceID(deviceInfoSet, &deviceInfo)
			if len(instanceID) < len(wintunDevicePrefix) ||
				!strings.EqualFold(instanceID[:len(wintunDevicePrefix)], wintunDevicePrefix) {
				continue
			}
			if isDeviceStarted(&deviceInfo) {
				continue
			}
			if removeDevice(deviceInfoSet, &deviceInfo) {
				removedCount++
				removedInPass = true
				break
			}
		}

		pSetupDiDestroyDeviceInfoList.Call(deviceInfoSet)
		if !removedInPass {
			break
		}
	}

	return removedCount
}
