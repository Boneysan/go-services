package main

import (
	"testing"
)

func TestExtractCMessageName(t *testing.T) {
	// Fake CMessage: 4-byte header (0), uint32 len + "DELTA" + payload
	header := []byte{0, 0, 0, 0}
	name := []byte("DELTA")
	nameLen := []byte{5, 0, 0, 0} // little endian 5
	payload := []byte{1, 2, 3}
	data := append(header, nameLen...)
	data = append(data, name...)
	data = append(data, payload...)

	nameOut, off := extractCMessageName(data)
	if nameOut != "DELTA" {
		t.Errorf("name = %q, want DELTA", nameOut)
	}
	expectedOff := 4 + 4 + 5 // header + len + name
	if off != expectedOff {
		t.Errorf("off = %d, want %d", off, expectedOff)
	}
}

func TestExtractCMessageName_TooShort(t *testing.T) {
	data := []byte{0, 0, 0, 0, 1, 0, 0, 0} // header + len=1 but no data
	_, off := extractCMessageName(data)
	if off != 0 {
		t.Errorf("expected off=0 for short data, got %d", off)
	}
}
