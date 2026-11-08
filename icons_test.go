package main

import "testing"

func TestEmbeddedIconsLoad(t *testing.T) {
	if len(parseIconEntries(iconData)) == 0 {
		t.Fatal("no icon entries")
	}
	for _, size := range []int{16, 20, 24, 32, 48, 256} {
		icon := loadEmbeddedIcon(iconData, size, size)
		if icon == 0 {
			t.Errorf("failed to load %d px icon", size)
			continue
		}
		pDestroyIcon.Call(icon)
	}
}

func TestEmbeddedIconSizes(t *testing.T) {
	available := map[int]bool{}
	for _, entry := range parseIconEntries(iconData) {
		available[entry.width] = true
	}
	for _, size := range []int{16, 20, 24, 32, 48, 256} {
		if !available[size] {
			t.Errorf("missing %d px frame", size)
		}
	}
}
