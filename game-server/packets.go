package main

import (
	"encoding/binary"
	"math"
)

const (
	PktJoinReq         = 0x01
	PktJoinAck         = 0x02
	PktLeave           = 0x03
	PktPlayerInput     = 0x04
	PktGameState       = 0x05
	PktPing            = 0x06
	PktPong            = 0x07
	PktChatSend        = 0x08
	PktChatBroadcast   = 0x09
	PktShoot           = 0x0A
	PktMapData         = 0x0C
	PktEntityDestroyed = 0x0D
	PktSpawnUnit       = 0x0E
	PktSwitchControl   = 0x0F
)

func makeHeader(pktType, senderID uint8, seq uint16) []byte {
	h := make([]byte, 4)
	h[0] = pktType
	h[1] = senderID
	binary.LittleEndian.PutUint16(h[2:], seq)
	return h
}

func putFloat32(buf []byte, offset int, val float32) {
	binary.LittleEndian.PutUint32(buf[offset:], math.Float32bits(val))
}

func getFloat32(buf []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(buf[offset:]))
}
