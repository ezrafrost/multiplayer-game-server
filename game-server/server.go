package main

import (
	"fmt"
	"net"
	"sync"
)

type Session struct {
	PlayerID     uint8
	Addr         *net.UDPAddr
	ControlledID uint8 // entity this player is currently controlling
}

type Server struct {
	conn     *net.UDPConn
	game     *Game
	worldMap *Map
	sessions map[string]*Session // addr string -> session
	byID     map[uint8]*Session  // player_id -> session
	mu       sync.Mutex
	nextPID  uint8
}

func newServer(conn *net.UDPConn, game *Game, worldMap *Map) *Server {
	return &Server{
		conn:     conn,
		game:     game,
		worldMap: worldMap,
		sessions: make(map[string]*Session),
		byID:     make(map[uint8]*Session),
		nextPID:  1,
	}
}

func (s *Server) broadcast(pkt []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		s.conn.WriteToUDP(pkt, sess.Addr)
	}
}

func (s *Server) handle(data []byte, addr *net.UDPAddr) {
	if len(data) < 4 {
		return
	}
	pktType := data[0]
	playerID := data[1]
	payload := data[4:]

	switch pktType {
	case PktJoinReq:
		s.handleJoin(addr)
	case PktLeave:
		s.handleLeave(playerID)
	case PktPlayerInput:
		s.handlePlayerInput(playerID, payload)
	case PktPing:
		s.conn.WriteToUDP(makeHeader(PktPong, playerID, 0), addr)
	case PktChatSend:
		s.handleChat(playerID, payload)
	case PktShoot:
		s.handleShoot(playerID, payload)
	case PktSpawnUnit:
		s.handleSpawnUnit(playerID)
	case PktSwitchControl:
		s.handleSwitchControl(playerID)
	}
}

// controlledEntityIDs returns the set of entity IDs currently driven by a player.
func (s *Server) controlledEntityIDs() map[uint8]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make(map[uint8]bool, len(s.sessions))
	for _, sess := range s.sessions {
		ids[sess.ControlledID] = true
	}
	return ids
}

// handleEntityDestroyed is called when a non-player entity is removed.
// If a player was controlling it, switches them to another unit or their player entity.
func (s *Server) handleEntityDestroyed(entityID uint8) {
	var affectedPlayerID uint8
	s.mu.Lock()
	for _, sess := range s.sessions {
		if sess.ControlledID == entityID {
			affectedPlayerID = sess.PlayerID
			break
		}
	}
	s.mu.Unlock()

	if affectedPlayerID == 0 {
		return
	}

	// Prefer another unit; fall back to the player entity (always alive after respawn)
	nextID, found := s.game.findAnyUnit(affectedPlayerID)
	if !found {
		nextID, found = s.game.findPlayerEntity(affectedPlayerID)
		if !found {
			return
		}
	}

	s.mu.Lock()
	if sess, ok := s.byID[affectedPlayerID]; ok {
		sess.ControlledID = nextID
	}
	s.mu.Unlock()

	s.sendToPlayer(affectedPlayerID, append(makeHeader(PktSwitchControl, 0, 0), nextID))
}

func (s *Server) sendToPlayer(playerID uint8, pkt []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.byID[playerID]; ok {
		s.conn.WriteToUDP(pkt, sess.Addr)
	}
}

func (s *Server) handleSwitchControl(playerID uint8) {
	s.mu.Lock()
	sess, ok := s.byID[playerID]
	if !ok {
		s.mu.Unlock()
		return
	}
	currentID := sess.ControlledID
	s.mu.Unlock()

	nextID, found := s.game.nextOwnedEntity(playerID, currentID)
	if !found || nextID == currentID {
		return
	}

	s.mu.Lock()
	if sess2, ok2 := s.byID[playerID]; ok2 {
		sess2.ControlledID = nextID
	}
	s.mu.Unlock()

	s.sendToPlayer(playerID, append(makeHeader(PktSwitchControl, 0, 0), nextID))
}

func (s *Server) handlePlayerDeath(ownerID uint8) {
	unitID, found := s.game.findAnyUnit(ownerID)
	if !found {
		return // player entity already respawned in tick(); nothing extra to do
	}

	s.mu.Lock()
	if sess, ok := s.byID[ownerID]; ok {
		sess.ControlledID = unitID
	}
	s.mu.Unlock()

	s.sendToPlayer(ownerID, append(makeHeader(PktSwitchControl, 0, 0), unitID))
}

func (s *Server) handleJoin(addr *net.UDPAddr) {
	s.mu.Lock()
	key := addr.String()
	if _, exists := s.sessions[key]; exists {
		s.mu.Unlock()
		return
	}
	pid := s.nextPID
	s.nextPID++
	sess := &Session{PlayerID: pid, Addr: addr}
	s.sessions[key] = sess
	s.byID[pid] = sess
	s.mu.Unlock()

	// Spawn the player entity outside the server lock
	e := &Entity{
		Type:    EntityPlayer,
		OwnerID: pid,
		X:       SpawnX,
		Y:       SpawnY,
		Health:  PlayerHealth,
	}
	entityID := s.game.addEntity(e)

	s.mu.Lock()
	sess.ControlledID = entityID
	s.mu.Unlock()

	// JOIN_ACK payload: [player_id][entity_id]
	s.conn.WriteToUDP(append(makeHeader(PktJoinAck, 0, 0), pid, entityID), addr)

	// Send map data immediately after join
	s.conn.WriteToUDP(append(makeHeader(PktMapData, 0, 0), s.worldMap.ToBytes()...), addr)

	fmt.Printf("Player %d joined (entity %d) from %s\n", pid, entityID, addr)
}

func (s *Server) handleLeave(playerID uint8) {
	s.mu.Lock()
	sess, ok := s.byID[playerID]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.sessions, sess.Addr.String())
	delete(s.byID, playerID)
	s.mu.Unlock()

	s.game.removeByOwner(playerID)
	fmt.Printf("Player %d left\n", playerID)
}

func (s *Server) handlePlayerInput(playerID uint8, payload []byte) {
	if len(payload) < 8 {
		return
	}
	s.mu.Lock()
	sess, ok := s.byID[playerID]
	if !ok {
		s.mu.Unlock()
		return
	}
	controlledID := sess.ControlledID
	s.mu.Unlock()

	velX := getFloat32(payload, 0) * PlayerSpeed
	velY := getFloat32(payload, 4) * PlayerSpeed
	s.game.setVelocity(controlledID, velX, velY)
}

func (s *Server) handleChat(playerID uint8, payload []byte) {
	pkt := append(makeHeader(PktChatBroadcast, 0, 0), playerID)
	pkt = append(pkt, payload...)
	s.broadcast(pkt)
	fmt.Printf("Player %d: %s\n", playerID, string(payload))
}

func (s *Server) handleSpawnUnit(playerID uint8) {
	s.mu.Lock()
	sess, ok := s.byID[playerID]
	if !ok {
		s.mu.Unlock()
		return
	}
	controlledID := sess.ControlledID
	s.mu.Unlock()

	s.game.spawnUnit(playerID, controlledID)
}

func (s *Server) handleShoot(playerID uint8, payload []byte) {
	if len(payload) < 8 {
		return
	}
	s.mu.Lock()
	sess, ok := s.byID[playerID]
	if !ok {
		s.mu.Unlock()
		return
	}
	controlledID := sess.ControlledID
	s.mu.Unlock()

	dirX := getFloat32(payload, 0)
	dirY := getFloat32(payload, 4)
	s.game.tryShoot(controlledID, dirX, dirY, PlayerAttackCooldown)
}
