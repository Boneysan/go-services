package main

import (
	"encoding/binary"
	"testing"
)

func TestReadNeLString(t *testing.T) {
	// "hello" as NeL string: len=5 + bytes
	data := []byte{5, 0, 0, 0, 'h', 'e', 'l', 'l', 'o'}
	pos := 0
	s, n, err := readNeLString(data, &pos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "hello" {
		t.Errorf("got %q, want hello", s)
	}
	if n != 5 {
		t.Errorf("string len %d, want 5", n)
	}
	// Note: readNeLString advances *pos past the 4-byte length, but NOT the content.
	// Callers do *pos += n for the content bytes.
	if pos != 4 {
		t.Errorf("pos after len %d, want 4", pos)
	}
	// Simulate caller advancing for content
	pos += n
	if pos != 9 {
		t.Errorf("pos after content %d, want 9", pos)
	}
}

func TestParseTParamValue(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want TParamValue
	}{
		{
			name: "uint32",
			data: append([]byte{byte(spt_uint32)}, binary.LittleEndian.AppendUint32(nil, 42)...),
			want: TParamValue{Type: spt_uint32, UInt32: 42},
		},
		{
			name: "string",
			data: func() []byte {
				b := []byte{byte(spt_string)}
				b = binary.LittleEndian.AppendUint32(b, 5)
				b = append(b, []byte("hello")...)
				return b
			}(),
			want: TParamValue{Type: spt_string, String: "hello"},
		},
		{
			name: "entityId",
			data: append([]byte{byte(spt_entityId)}, make([]byte, 16)...), // all zero eid for simplicity
			want: TParamValue{Type: spt_entityId, EntityId: [16]byte{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos := 0
			got, err := parseTParamValue(tt.data, &pos)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if got.Type != tt.want.Type {
				t.Errorf("type = %v, want %v", got.Type, tt.want.Type)
			}
			// spot check value
			switch tt.want.Type {
			case spt_uint32:
				if got.UInt32 != tt.want.UInt32 {
					t.Errorf("uint32 = %d, want %d", got.UInt32, tt.want.UInt32)
				}
			case spt_string:
				if got.String != tt.want.String {
					t.Errorf("string = %q, want %q", got.String, tt.want.String)
				}
			}
		})
	}
}

func TestTryParseCMessage(t *testing.T) {
	// Build a fake CMessage: 4-byte header (0), uint32 len + "RC" + some payload
	header := []byte{0, 0, 0, 0}
	name := []byte("RC")
	nameLen := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	payload := []byte{1, 2, 3} // dummy payload
	data := append(header, nameLen...)
	data = append(data, name...)
	data = append(data, payload...)

	consumed, nameOut, ok := tryParseCMessage(data)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if nameOut != "RC" {
		t.Errorf("name = %q, want RC", nameOut)
	}
	if consumed != 4+4+2 { // header + len + "RC"
		t.Errorf("consumed = %d, want 10", consumed)
	}
}

func TestParseVectorTLogInfo_Simple(t *testing.T) {
	// Construct exactly as the serial order the parsers expect.
	// count=1
	var data []byte
	data = binary.LittleEndian.AppendUint32(data, 1)

	// name "FOO" (len + bytes)
	data = binary.LittleEndian.AppendUint32(data, 3)
	data = append(data, []byte("FOO")...)

	// ts (little endian 123)
	data = binary.LittleEndian.AppendUint32(data, 123)

	// params count=0
	data = binary.LittleEndian.AppendUint32(data, 0)

	// listparams count=0
	data = binary.LittleEndian.AppendUint32(data, 0)

	pos := 0
	infos, err := parseVectorTLogInfo(data, &pos)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("len = %d, want 1", len(infos))
	}
	if infos[0].LogName != "FOO" {
		t.Errorf("LogName = %q, want FOO", infos[0].LogName)
	}
	if infos[0].TimeStamp != 123 {
		t.Errorf("TimeStamp = %d, want 123", infos[0].TimeStamp)
	}
}
