package main

import (
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// decodeTextBytes converts the encodings commonly produced by Windows tools
// (UTF-8 with or without BOM, UTF-16 LE/BE with BOM, or ANSI) to UTF-8. The
// bool reports whether a UTF-8 BOM was present.
func decodeTextBytes(data []byte) (string, bool) {
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		return string(data[3:]), true
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return utf16BytesToString(data[2:], false), false
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return utf16BytesToString(data[2:], true), false
	case !utf8.Valid(data):
		return ansiToUTF8(data), false
	default:
		return string(data), false
	}
}

func utf16BytesToString(data []byte, bigEndian bool) string {
	u := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			u = append(u, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return windows.UTF16ToString(u)
}

var pMultiByteToWideChar = kernel32.NewProc("MultiByteToWideChar")

func ansiToUTF8(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	need, _, _ := pMultiByteToWideChar.Call(0, 0, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, 0) // CP_ACP
	if need == 0 {
		return string(data)
	}
	buf := make([]uint16, int(need))
	pMultiByteToWideChar.Call(0, 0, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}
