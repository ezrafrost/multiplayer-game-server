package main

import (
	"encoding/binary"
	"math"
	"testing"
)

// The Godot client re-implements this header layout and float encoding by hand,
// so these tests are the contract that keeps both sides in sync.

func TestMakeHeader(t *testing.T) {
	h := makeHeader(PktGameState, 7, 0x1234)
	if len(h) != 4 {
		t.Fatalf("header length = %d, want 4", len(h))
	}
	if h[0] != PktGameState {
		t.Errorf("type byte = %#x, want %#x", h[0], PktGameState)
	}
	if h[1] != 7 {
		t.Errorf("sender byte = %d, want 7", h[1])
	}
	if got := binary.LittleEndian.Uint16(h[2:]); got != 0x1234 {
		t.Errorf("seq = %#x, want 0x1234", got)
	}
}

func TestFloat32RoundTrip(t *testing.T) {
	buf := make([]byte, 8)
	for _, v := range []float32{0, 1, -1, 3.14159, 1e6, -2.5e-3, math.MaxFloat32} {
		putFloat32(buf, 2, v)
		if got := getFloat32(buf, 2); got != v {
			t.Errorf("round trip of %v produced %v", v, got)
		}
	}
}
