package main

const (
	EntityPlayer = uint8(1)
	EntityUnit   = uint8(2)
	EntityBullet = uint8(3)
)

const (
	BulletTTL            = 60 // ticks (~3s at 20Hz)
	BulletSpeed          = float32(300)
	PlayerSpeed          = float32(100)
	PlayerHealth         = int16(100)
	UnitHealth           = int16(75)
	EntityRadius         = float32(9) // collision half-size (slightly under half of render size)
	BulletDamage         = int16(25)  // 4 hits to kill a full-health entity
	PlayerAttackCooldown = 20         // ticks between player shots (1 s at 20 Hz)

	// Spawn in the centre of tile row 8 (upper section, safely above the dividing wall)
	SpawnX = float32(MapWidth*TileSize) / 2
	SpawnY = float32(8*TileSize + TileSize/2)
)

type Entity struct {
	ID             uint8
	Type           uint8
	OwnerID        uint8
	X, Y           float32
	VelX           float32
	VelY           float32
	Health         int16
	TTL            int // ticks remaining; 0 = no expiry
	AttackCooldown int // ticks until next shot (units only)
}
