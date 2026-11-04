package main

import (
	_ "embed"
	"encoding/binary"
	"runtime"
	"unsafe"
)

//go:embed icon.ico
var iconEnabledData []byte

//go:embed icon_disabled.ico
var iconDisabledData []byte

//go:embed icon_tun.ico
var iconTunData []byte

type iconDirEntry struct {
	width    int
	height   int
	bytes    uint32
	offset   uint32
	bitCount uint16
}

func parseIconEntries(data []byte) []iconDirEntry {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:]) != 0 || binary.LittleEndian.Uint16(data[2:]) != 1 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if count <= 0 || len(data) < 6+count*16 {
		return nil
	}
	entries := make([]iconDirEntry, 0, count)
	for i := 0; i < count; i++ {
		raw := data[6+i*16:]
		w := int(raw[0])
		h := int(raw[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		entries = append(entries, iconDirEntry{
			width:    w,
			height:   h,
			bitCount: binary.LittleEndian.Uint16(raw[6:]),
			bytes:    binary.LittleEndian.Uint32(raw[8:]),
			offset:   binary.LittleEndian.Uint32(raw[12:]),
		})
	}
	return entries
}

func pickIconEntry(entries []iconDirEntry, cx, cy int) (iconDirEntry, bool) {
	if len(entries) == 0 {
		return iconDirEntry{}, false
	}

	for _, entry := range entries {
		if entry.width == cx && entry.height == cy {
			return entry, true
		}
	}

	var best iconDirEntry
	found := false
	for _, entry := range entries {
		if entry.width >= cx && entry.height >= cy {
			if !found || entry.width < best.width || (entry.width == best.width && entry.height < best.height) {
				best = entry
				found = true
			}
		}
	}
	if found {
		return best, true
	}

	best = entries[0]
	for _, entry := range entries[1:] {
		if entry.width > best.width || (entry.width == best.width && entry.height > best.height) {
			best = entry
		}
	}
	return best, true
}

func loadEmbeddedIcon(data []byte, cx, cy int) uintptr {
	entries := parseIconEntries(data)
	entry, ok := pickIconEntry(entries, cx, cy)
	if !ok {
		return 0
	}
	start := int(entry.offset)
	end := start + int(entry.bytes)
	if start < 0 || end > len(data) || start >= end {
		return 0
	}

	bits := &data[start]
	icon, _, _ := pCreateIconFromResEx.Call(
		uintptr(unsafe.Pointer(bits)),
		uintptr(entry.bytes),
		1,
		0x00030000,
		uintptr(cx),
		uintptr(cy),
		0)
	runtime.KeepAlive(data)
	return icon
}

func loadApplicationIcon(cx, cy int) uintptr {
	icon := loadEmbeddedIcon(iconEnabledData, cx, cy)
	if icon != 0 {
		return icon
	}
	r, _, _ := pLoadIconW.Call(0, idiApplication)
	return r
}

func getSystemMetric(index int) int {
	r, _, _ := pGetSystemMetrics.Call(uintptr(index))
	return int(int32(r))
}
