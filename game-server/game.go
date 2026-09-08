package main

import (
	"encoding/binary"
	"math"
	"sort"
	"sync"
)

type Game struct {
	entities map[uint8]*Entity
	mu       sync.Mutex
	nextID   uint8
	worldMap *Map
}

func newGame(worldMap *Map) *Game {
	return &Game{
		entities: make(map[uint8]*Entity),
		nextID:   1,
		worldMap: worldMap,
	}
}

// addEntity assigns an ID and registers the entity. Returns the assigned ID.
func (g *Game) addEntity(e *Entity) uint8 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.addEntityLocked(e)
	return e.ID
}

// addEntityLocked is addEntity without locking — caller must hold g.mu.
func (g *Game) addEntityLocked(e *Entity) {
	e.ID = g.nextID
	g.nextID++
	g.entities[e.ID] = e
}

func (g *Game) removeEntity(id uint8) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entities, id)
}

func (g *Game) removeByOwner(ownerID uint8) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, e := range g.entities {
		if e.OwnerID == ownerID {
			delete(g.entities, id)
		}
	}
}

func (g *Game) setVelocity(id uint8, vx, vy float32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.entities[id]; ok {
		e.VelX = vx
		e.VelY = vy
	}
}

func (g *Game) getPosition(id uint8) (x, y float32, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, exists := g.entities[id]; exists {
		return e.X, e.Y, true
	}
	return 0, 0, false
}

// tick advances the simulation by one step (called at 20 Hz).
// Returns: broadcast packets, OwnerIDs of player entities that died,
// and entity IDs of non-player entities that were destroyed.
func (g *Game) tick() ([][]byte, []uint8, []uint8) {
	const dt = float32(1.0 / 20.0)

	g.mu.Lock()
	defer g.mu.Unlock()

	var expired []uint8
	var extra [][]byte
	var deaths []uint8
	var destroyedIDs []uint8

	for _, e := range g.entities {
		if e.Type == EntityBullet {
			newX := e.X + e.VelX*dt
			newY := e.Y + e.VelY*dt

			hit := false
			if g.worldMap.solidAt(newX, newY) {
				hit = true
			} else {
				e.X = newX
				e.Y = newY
				// Check bullet against every non-bullet entity from a different owner
				for _, target := range g.entities {
					if target.Type == EntityBullet || target.OwnerID == e.OwnerID {
						continue
					}
					dx := target.X - e.X
					dy := target.Y - e.Y
					if dx*dx+dy*dy < EntityRadius*EntityRadius {
						hit = true
						target.Health -= BulletDamage
						if target.Health <= 0 {
							if target.Type == EntityPlayer {
								target.X, target.Y = SpawnX, SpawnY
								target.VelX, target.VelY = 0, 0
								target.Health = PlayerHealth
								deaths = append(deaths, target.OwnerID)
							} else {
								expired = append(expired, target.ID)
								destroyedIDs = append(destroyedIDs, target.ID)
								extra = append(extra, append(makeHeader(PktEntityDestroyed, 0, 0), target.ID))
							}
						}
						break
					}
				}
			}

			if hit {
				expired = append(expired, e.ID)
				continue
			}
			if e.TTL > 0 {
				e.TTL--
				if e.TTL == 0 {
					expired = append(expired, e.ID)
				}
			}
		} else {
			// Axis-separated collision so entities slide along walls
			newX := e.X + e.VelX*dt
			if !g.worldMap.solidAABB(newX, e.Y, EntityRadius) {
				e.X = newX
			}
			newY := e.Y + e.VelY*dt
			if !g.worldMap.solidAABB(e.X, newY, EntityRadius) {
				e.Y = newY
			}
			// Decrement attack cooldown for all non-bullet entities
			if e.AttackCooldown > 0 {
				e.AttackCooldown--
			}
		}
	}

	for _, id := range expired {
		delete(g.entities, id)
	}
	g.separateEntitiesLocked()
	return extra, deaths, destroyedIDs
}

// findPlayerEntity returns the ID of the EntityPlayer owned by ownerID.
func (g *Game) findPlayerEntity(ownerID uint8) (uint8, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, e := range g.entities {
		if e.OwnerID == ownerID && e.Type == EntityPlayer {
			return e.ID, true
		}
	}
	return 0, false
}

// nextOwnedEntity cycles to the entity after currentID among all non-bullet entities
// owned by ownerID (sorted by ID for deterministic order).
func (g *Game) nextOwnedEntity(ownerID, currentID uint8) (uint8, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var ids []uint8
	for _, e := range g.entities {
		if e.OwnerID == ownerID && e.Type != EntityBullet {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return 0, false
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for i, id := range ids {
		if id == currentID {
			return ids[(i+1)%len(ids)], true
		}
	}
	return ids[0], true
}

// findAnyUnit returns the ID of any unit owned by ownerID.
func (g *Game) findAnyUnit(ownerID uint8) (uint8, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, e := range g.entities {
		if e.OwnerID == ownerID && e.Type == EntityUnit {
			return e.ID, true
		}
	}
	return 0, false
}

// separateEntitiesLocked pushes overlapping non-bullet entities apart.
// Caller must hold g.mu.
func (g *Game) separateEntitiesLocked() {
	const minDist = EntityRadius * 2

	var solids []*Entity
	for _, e := range g.entities {
		if e.Type != EntityBullet {
			solids = append(solids, e)
		}
	}

	for i := 0; i < len(solids); i++ {
		for j := i + 1; j < len(solids); j++ {
			a, b := solids[i], solids[j]
			dx := b.X - a.X
			dy := b.Y - a.Y
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			if dist >= minDist {
				continue
			}
			var nx, ny float32
			if dist < 0.001 {
				nx, ny = 1, 0 // arbitrary push direction for perfect overlap
			} else {
				nx = dx / dist
				ny = dy / dist
			}
			push := (minDist - dist) * 0.5
			aNx, aNy := a.X-nx*push, a.Y-ny*push
			bNx, bNy := b.X+nx*push, b.Y+ny*push
			if !g.worldMap.solidAABB(aNx, aNy, EntityRadius) {
				a.X, a.Y = aNx, aNy
			}
			if !g.worldMap.solidAABB(bNx, bNy, EntityRadius) {
				b.X, b.Y = bNx, bNy
			}
		}
	}
}

// spawnUnit creates a unit for ownerID near the entity it controls.
// Locks once so position lookup and entity creation are atomic.
func (g *Game) spawnUnit(ownerID, controlledID uint8) {
	g.mu.Lock()
	defer g.mu.Unlock()

	controlled, ok := g.entities[controlledID]
	if !ok {
		return
	}
	sx, sy := g.findSpawnNearLocked(controlled.X, controlled.Y)
	g.addEntityLocked(&Entity{
		Type:    EntityUnit,
		OwnerID: ownerID,
		X:       sx,
		Y:       sy,
		Health:  UnitHealth,
	})
}

// findSpawnNearLocked returns the first non-wall position among the 8 tile-spaced
// neighbours of (x, y), falling back to (x, y) if all are blocked.
// Caller must hold g.mu.
func (g *Game) findSpawnNearLocked(x, y float32) (float32, float32) {
	offsets := [][2]float32{
		{TileSize, 0}, {-TileSize, 0}, {0, TileSize}, {0, -TileSize},
		{TileSize, TileSize}, {-TileSize, TileSize}, {TileSize, -TileSize}, {-TileSize, -TileSize},
	}
	for _, off := range offsets {
		nx, ny := x+off[0], y+off[1]
		if !g.worldMap.solidAABB(nx, ny, EntityRadius) {
			return nx, ny
		}
	}
	return x, y
}

// tryShoot fires a bullet from the given entity if its cooldown has expired.
// Caller must NOT hold g.mu.
func (g *Game) tryShoot(shooterID uint8, dirX, dirY float32, cooldown int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	shooter, ok := g.entities[shooterID]
	if !ok || shooter.AttackCooldown > 0 {
		return
	}
	g.addEntityLocked(&Entity{
		Type:    EntityBullet,
		OwnerID: shooter.OwnerID,
		X:       shooter.X,
		Y:       shooter.Y,
		VelX:    dirX * BulletSpeed,
		VelY:    dirY * BulletSpeed,
		Health:  1,
		TTL:     BulletTTL,
	})
	shooter.AttackCooldown = cooldown
}

// buildGameState encodes all entities into a GAME_STATE packet.
// Per entity: id(1) + type(1) + owner_id(1) + x(4) + y(4) + health(2) = 13 bytes
func (g *Game) buildGameState() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()

	pkt := make([]byte, 4+len(g.entities)*13)
	copy(pkt, makeHeader(PktGameState, 0, 0))
	offset := 4
	for _, e := range g.entities {
		pkt[offset] = e.ID
		pkt[offset+1] = e.Type
		pkt[offset+2] = e.OwnerID
		putFloat32(pkt, offset+3, e.X)
		putFloat32(pkt, offset+7, e.Y)
		binary.LittleEndian.PutUint16(pkt[offset+11:], uint16(e.Health))
		offset += 13
	}
	return pkt
}
