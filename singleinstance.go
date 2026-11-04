package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

const mutexSddl = "D:(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;AU)"

var singleInstanceHandle windows.Handle

func acquireSingleInstance(mutexName string, waitIfExists bool, waitMs uint32) bool {
	sd, err := windows.SecurityDescriptorFromString(mutexSddl)
	if err != nil {
		return false
	}

	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}

	handle, err := windows.CreateMutex(&sa, true, utf16Ptr(mutexName))
	if handle == 0 {
		return false
	}

	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if !waitIfExists {
			_ = windows.CloseHandle(handle)
			return false
		}
		event, waitErr := windows.WaitForSingleObject(handle, waitMs)
		if waitErr != nil || (event != windows.WAIT_OBJECT_0 && event != windows.WAIT_ABANDONED) {
			_ = windows.CloseHandle(handle)
			return false
		}
	} else if err != nil {
		_ = windows.CloseHandle(handle)
		return false
	}

	singleInstanceHandle = handle
	return true
}
