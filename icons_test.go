package main

import "testing"

func TestEmbeddedIconsLoad(t *testing.T) {
	icons := map[string][]byte{
		"enabled":  iconEnabledData,
		"disabled": iconDisabledData,
		"tun":      iconTunData,
	}
	for name, data := range icons {
		if len(parseIconEntries(data)) == 0 {
			t.Fatalf("%s: no icon entries", name)
		}
		for _, size := range []int{16, 32, 48, 256} {
			icon := loadEmbeddedIcon(data, size, size)
			if icon == 0 {
				t.Errorf("%s: failed to load %d px icon", name, size)
				continue
			}
			pDestroyIcon.Call(icon)
		}
	}
}
