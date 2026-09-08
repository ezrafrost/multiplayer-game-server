package main

import "math"

const (
	AIDetectionRange = float32(200) // pixels — enemy must be this close to trigger attack mode
	AIFollowRadius   = float32(80)  // pixels — unit stops following owner once within this distance
	AIUnitSpeed      = float32(60)  // pixels per second
	AIAttackCooldown = 40           // ticks between shots (2 s at 20 Hz)
)

// runAI updates velocity and fires bullets for every unit entity not currently
// controlled by a player. Must be called before tick().
func (g *Game) runAI(playerControlled map[uint8]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, unit := range g.entities {
		if unit.Type != EntityUnit {
			continue
		}
		if playerControlled[unit.ID] {
			continue // player is driving this unit; leave velocity alone
		}
		enemy, dist := g.nearestEnemy(unit)
		if enemy != nil && dist < AIDetectionRange {
			g.attackBehavior(unit, enemy, dist)
		} else {
			g.followBehavior(unit)
		}
	}
}

func (g *Game) attackBehavior(unit, enemy *Entity, dist float32) {
	dx := enemy.X - unit.X
	dy := enemy.Y - unit.Y
	unit.VelX = (dx / dist) * AIUnitSpeed
	unit.VelY = (dy / dist) * AIUnitSpeed

	if unit.AttackCooldown == 0 {
		g.addEntityLocked(&Entity{
			Type:    EntityBullet,
			OwnerID: unit.OwnerID,
			X:       unit.X,
			Y:       unit.Y,
			VelX:    (dx / dist) * BulletSpeed,
			VelY:    (dy / dist) * BulletSpeed,
			Health:  1,
			TTL:     BulletTTL,
		})
		unit.AttackCooldown = AIAttackCooldown
	}
}

func (g *Game) followBehavior(unit *Entity) {
	owner := g.ownerPlayerEntity(unit.OwnerID)
	if owner == nil {
		unit.VelX, unit.VelY = 0, 0
		return
	}
	dx := owner.X - unit.X
	dy := owner.Y - unit.Y
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if dist > AIFollowRadius {
		unit.VelX = (dx / dist) * AIUnitSpeed
		unit.VelY = (dy / dist) * AIUnitSpeed
	} else {
		unit.VelX, unit.VelY = 0, 0
	}
}

// nearestEnemy returns the closest entity not owned by unit's owner (bullets excluded).
func (g *Game) nearestEnemy(unit *Entity) (*Entity, float32) {
	var nearest *Entity
	nearestDist := float32(math.MaxFloat32)
	for _, e := range g.entities {
		if e.OwnerID == unit.OwnerID || e.Type == EntityBullet {
			continue
		}
		dx := e.X - unit.X
		dy := e.Y - unit.Y
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if dist < nearestDist {
			nearest = e
			nearestDist = dist
		}
	}
	return nearest, nearestDist
}

// ownerPlayerEntity returns the EntityPlayer owned by ownerID, or nil.
func (g *Game) ownerPlayerEntity(ownerID uint8) *Entity {
	for _, e := range g.entities {
		if e.Type == EntityPlayer && e.OwnerID == ownerID {
			return e
		}
	}
	return nil
}
