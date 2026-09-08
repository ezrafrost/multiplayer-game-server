# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Components

- **`game-server/`** — authoritative game server. Plain Go (`module game-server`, Go 1.26), no third-party dependencies, `package main` split across files.
- **`game-client/multiplayer-game/`** — Godot 4.6 client (Forward+, d3d12). All logic lives in `scene_1.gd` attached to the root `Node2D` of `scene1.tscn`. Pure `_draw()` rendering, no TileMap/sprites yet.
- **`shared/MAPS.txt`** — scratch pad of hand-drawn `mapLayout` arrays, not code. The live map is `mapLayout` in `game-server/map.go`.

`docs/ARCHITECTURE.md` is the detailed architecture + protocol reference. The root
`.gitignore` excludes the Go binary (`game-server/game-server.exe`) and editor/OS noise.

## Commands

```sh
# Server (from game-server/)
go run .                 # build + run, listens UDP :8080
go build -o game-server.exe && ./game-server.exe
go test ./...            # map_test.go (collision/serialisation) + packets_test.go (wire format)
go vet ./...

# Client
# Open game-client/multiplayer-game/ in the Godot 4.6 editor and run, or:
godot --path game-client/multiplayer-game
```

The client hard-codes `127.0.0.1:8080` in `scene_1.gd:_ready`. Start the server first, then run one or more client instances.

The Go server has focused unit tests (`go test ./...`); there is no linter beyond `go vet`, and no CI.

## Architecture

### Authoritative simulation, fixed 20 Hz tick

`main.go` runs one goroutine on a `time.NewTicker(time.Second / 20)`. Each tick, in order:
1. `game.runAI(controlledIDs)` — sets velocity/fires bullets for un-possessed units (`ai.go`).
2. `game.tick()` — the **only** place positions change: integrates velocity, does map + entity collision, applies bullet damage, expires bullets, respawns dead players, separates overlapping entities. Returns `(extraPackets, playerDeaths, destroyedEntityIDs)`.
3. Broadcast those packets, then `server.handlePlayerDeath` / `handleEntityDestroyed` for control reassignment.
4. Broadcast `game.buildGameState()` (full entity snapshot).

**Packet handlers never move anything.** `handlePlayerInput`, `handleShoot`, `handleSpawnUnit` only record intent (velocity, cooldown timers, new entities) which the next `tick()` acts on. Incoming packets are each dispatched on their own goroutine (`go server.handle(...)` in `main.go`).

### Entities

One `Entity` struct (`entity.go`) for all three `Type`s: `EntityPlayer` (1), `EntityUnit` (2), `EntityBullet` (3). All game state is `Game.entities map[uint8]*Entity` keyed by an 8-bit ID. Tuning constants (speeds, health, radius, cooldowns, spawn point) live in `entity.go`; AI constants in `ai.go`.

### Control model

A player possesses exactly one entity at a time: `Session.ControlledID` in `server.go`. `PLAYER_INPUT`, `SHOOT`, `SPAWN_UNIT` all act on that entity. `SWITCH_CONTROL` cycles through the player's owned non-bullet entities (`game.nextOwnedEntity`). When the controlled entity dies or a player's own player-entity dies, the server picks a replacement and pushes `PKT_SWITCH_CONTROL` to just that client.

### Map

`mapLayout` in `map.go` (`MapWidth`×`MapHeight` of `0`=empty / `1`=wall / `2`=structure) is server-authoritative. Sent to each client once on join via `PKT_MAP_DATA`; the client only renders it and never checks collision. `solidAABB` does 4-corner box tests; non-bullet movement in `tick()` is axis-separated so entities slide along walls.

### Wire protocol

4-byte header: `[type:u8][senderID:u8][seq:u16 LE]`, then a type-specific payload. Floats are little-endian `float32`; helpers in `packets.go` (`makeHeader`, `putFloat32`, `getFloat32`) and mirrored GDScript in `scene_1.gd`.

Two hard invariants when touching the protocol:
- **Packet-type constants must stay identical** in `game-server/packets.go` and `game-client/multiplayer-game/scene_1.gd` (same for the `Entity*`/`Tile*` constants).
- **`GAME_STATE` is 13 bytes per entity** — `id(1) type(1) owner(1) x(4) y(4) health(2)`. Changing the layout means editing both `game.buildGameState()` and `_handle_game_state()` in lockstep.

### Concurrency

Two locks: `Server.mu` guards `sessions`/`byID`, `Game.mu` guards `entities`. Convention: a `...Locked` suffix (`addEntityLocked`, `separateEntitiesLocked`, `findSpawnNearLocked`) means the caller already holds `Game.mu`; the unsuffixed version takes it. Do server-lock work and game-lock work in separate critical sections — several handlers deliberately unlock `Server.mu` before calling into `Game`.

## Status

`README.md` lays out a 6-phase plan (server simulation → map → combat → unit spawning → unit AI → control switching) that is **fully implemented**. Its "next part planning" and "Graphics phase" notes are the remaining roadmap: join/leave/loading screens, varied spawn points, distinct unit types (size/speed/health/bullet behaviour), and real tile/sprite art on the client.
