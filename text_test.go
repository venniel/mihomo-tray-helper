package main

import "testing"

func TestDecodeTextBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
		bom  bool
	}{
		{"empty", nil, "", false},
		{"utf8", []byte("hello"), "hello", false},
		{"utf8 bom", append([]byte{0xEF, 0xBB, 0xBF}, "hello"...), "hello", true},
		{"utf16le", append([]byte{0xFF, 0xFE}, encodeUTF16LE("你好")...), "你好", false},
		{"utf16be", []byte{0xFE, 0xFF, 0x4F, 0x60, 0x59, 0x7D}, "你好", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, bom := decodeTextBytes(c.data)
			if got != c.want {
				t.Errorf("text = %q, want %q", got, c.want)
			}
			if bom != c.bom {
				t.Errorf("bom = %v, want %v", bom, c.bom)
			}
		})
	}
}
