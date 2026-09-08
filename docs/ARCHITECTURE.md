# Architecture

This document describes how the multiplayer game server and its Godot client actually
work today. It only covers things that exist in the code. Anything not yet built is
called out as such or left for the [roadmap in the README](../README.md#roadmap).

---

## 1. System overview

```
        Godot 4 client (game-client/multiplayer-game/scene_1.gd)
                    │
                    │  UDP datagrams, custom binary protocol, port 8080
                    ▼
        Go server (game-server/, package main)
                    │
        ┌───────────┴────────────┐
        │                        │
   networking layer         simulation layer
   (server.go, main.go)     (game.go, ai.go, map.go, entity.go)
```

The client and server are separate programs with no shared code. The wire protocol
(`game-server/packets.go` ⇄ the constants and decoders at the top of `scene_1.gd`) is
the entire contract between them.

### Responsibilities

| Concern | Owner |
|---------|-------|
| Player identity / sessions | Server (`server.go`) |
| Position, velocity integration | Server (`game.go` `tick`) |
| Collision (tiles and entities) | Server (`map.go`, `game.go`) |
| Health, damage, death, respawn | Server (`game.go` `tick`) |
| Bullet lifetime and hits | Server (`game.go` `tick`) |
| Unit AI | Server (`ai.go`) |
| Which entity a player controls | Server (`server.go` `Session.ControlledID`) |
| The map | Server (`map.go`), sent to clients once on join |
| Input sampling | Client |
| Rendering, camera | Client |
| Interpolation / prediction | *Neither — not implemented* |

The client holds a copy of the map for rendering only; it never runs collision.

---

## 2. Processes, goroutines, and shared state

`main.go` starts two concurrent activities plus one goroutine per inbound packet:

```
main goroutine
  └─ for { conn.ReadFromUDP(buf); go server.handle(copyOf(buf)) }   ← network read loop

tick goroutine  (time.NewTicker(time.Second / 20))
  └─ for range ticker.C {
         game.runAI(server.controlledEntityIDs())
         pkts, deaths, destroyed := game.tick()
         broadcast(pkts...); reassign control for deaths / destroyed
         broadcast(game.buildGameState())
     }

per-packet goroutines  (go server.handle(data, addr))
  └─ parse header → dispatch → mutate intent under a lock
```

### Shared state and locks

There are two independent mutexes:

| Mutex | Guards | Held by |
|-------|--------|---------|
| `Server.mu` | `sessions map[string]*Session`, `byID map[uint8]*Session`, `nextPID` | join/leave, dispatch, control lookups |
| `Game.mu` | `entities map[uint8]*Entity`, `nextID` | `tick`, `runAI`, all entity mutations |

The two locks are taken in **separate** critical sections, never nested. A recurring
pattern in `server.go` is: take `Server.mu`, read the field you need
(e.g. `sess.ControlledID`), release it, then call into `Game` which takes `Game.mu`.
This avoids a lock-ordering hazard between the network handlers and the tick loop.

Helper methods on `Game` come in pairs where it matters: `addEntity` takes `Game.mu`;
`addEntityLocked` assumes the caller already holds it. The `Locked` suffix is the
convention that keeps the locking discipline auditable. `tick`, `runAI`, `spawnUnit`,
and `tryShoot` each take the lock once and do all their work inside it, so a whole
tick's worth of AI + physics + snapshot encoding is internally consistent.

---

## 3. Connection lifecycle

There is no handshake beyond a single request/ack, and no explicit connection object —
a "session" is just an entry in a map keyed by the UDP address string.

### Join

```
client                                  server
  │  JOIN_REQ (type=0x01) ───────────►   handleJoin(addr)
  │                                        • dedupe on addr; assign player_id (nextPID++)
  │                                        • create Session{PlayerID, Addr}
  │                                        • spawn an Entity{Type: player, OwnerID: pid,
  │                                          X/Y = SpawnX/SpawnY, Health: PlayerHealth}
  │                                        • Session.ControlledID = that entity's id
  │  ◄─────────────── JOIN_ACK (player_id, entity_id)
  │  ◄─────────────── MAP_DATA (width, height, tiles…)
  │
  │  …then GAME_STATE broadcasts arrive every tick
```

The client sets `player_id` / `controlled_entity_id` from `JOIN_ACK` and starts sending
input on the next frame.

### Steady state

- **Client → server, every frame:** `PLAYER_INPUT` with a movement unit-vector
  (`scene_1.gd` `_send_player_input`). Also event-driven: `SHOOT` on click,
  `SPAWN_UNIT` on `E`, `SWITCH_CONTROL` on `F`, `CHAT_SEND` from `send_chat`.
- **Server → all clients, every tick:** `GAME_STATE` (full snapshot) plus any
  `ENTITY_DESTROYED` events produced that tick.
- **Server → one client:** `SWITCH_CONTROL` when the server changes what that player
  controls (see §6).

### Leave / disconnect

`handleLeave` exists: it removes both map entries and calls `game.removeByOwner`, which
deletes every entity owned by that player. **The client never sends `LEAVE`**, and there
is no timeout or keepalive, so in practice a player who closes the window stays in the
world until the server restarts. Closing this gap (client `LEAVE` on quit + server-side
idle timeout) is a tracked roadmap item.

---

## 4. The simulation tick (`game.go` `tick`)

Runs at a fixed `dt = 1/20 s`. One pass over all entities, under `Game.mu`:

**Bullets:**
1. Compute the next position.
2. If that position is solid in the tile map → mark the bullet expired.
3. Otherwise move it, then test it against every non-bullet entity with a *different*
   owner. On overlap (`dx² + dy² < EntityRadius²`): apply `BulletDamage`, expire the
   bullet, and if the target hit 0 HP:
   - **player** → teleport back to spawn, zero velocity, restore full health, and emit
     the owner's ID in the `deaths` list.
   - **unit / other** → expire it and emit `ENTITY_DESTROYED`.
4. Decrement `TTL`; expire at 0.

**Non-bullet entities (players + units):**
- Axis-separated move: try `X += VelX*dt` (revert if the new box is solid), then the
  same for `Y`. This is what lets an entity slide along a wall instead of stopping.
- Decrement `AttackCooldown`.

**After the loop:** delete all expired entities, then `separateEntitiesLocked` runs a
pairwise pass pushing any overlapping non-bullet entities apart (skipping a push that
would land inside a wall).

`tick` returns `([]extraPackets, []deaths, []destroyedIDs)`; `main.go` broadcasts the
packets and hands deaths/destroyed IDs to `server.go` for control reassignment.

### Collision model (`map.go`)

- The map is a flat `[]uint8` of `MapWidth*MapHeight`, row-major.
- `tileAt` returns `TileWall` for any out-of-bounds coordinate, so the world border
  needs no special-casing.
- `solidAt(x, y)` → tile is `TileWall` or `TileStructure`.
- `solidAABB(x, y, r)` tests the four corners of a square box — the cheap
  approximation the tick uses for entity/wall collision.

---

## 5. Entities

One struct for everything (`entity.go`):

```go
type Entity struct {
    ID, Type, OwnerID     uint8       // Type: 1 player, 2 unit, 3 bullet
    X, Y, VelX, VelY      float32
    Health                int16
    TTL                   int         // ticks; 0 = never expires (players, units)
    AttackCooldown        int         // ticks until this entity may fire again
}
```

- `OwnerID` is the player ID that owns the entity. Bullets inherit their shooter's
  owner, which is how friendly fire is skipped in both the tick and the AI.
- All entities live in `Game.entities`, keyed by an 8-bit ID from a monotonic counter.
  IDs are not currently recycled, so a very long session could exhaust the `uint8`
  space — noted, not yet addressed.
- Tuning lives entirely in constants: `entity.go` (speeds, health, radius, bullet TTL,
  cooldowns, spawn point) and `ai.go` (detection/follow ranges, AI speed, AI cooldown).

---

## 6. Player state and the possession model

A player does not have a fixed avatar. `Session.ControlledID` points at whichever entity
the player currently drives, and every player action routes through it:

- `PLAYER_INPUT` → `game.setVelocity(ControlledID, …)`
- `SHOOT` → `game.tryShoot(ControlledID, …)`
- `SPAWN_UNIT` → new unit is placed near `ControlledID` (`findSpawnNearLocked` scans the
  8 tile-spaced neighbours for a non-wall cell)

**Switching control** (`F` → `SWITCH_CONTROL`): `nextOwnedEntity` collects the player's
non-bullet entities, sorts by ID for a stable order, and returns the one after the
current. The server updates the session and sends `SWITCH_CONTROL` back with the new ID.

**Reassignment on death:**
- Controlled entity destroyed (`handleEntityDestroyed`) → switch the player to another
  owned unit, else to their player entity.
- Player entity itself dies (`handlePlayerDeath`) → the player entity is already
  respawned inside `tick`; if the player also owns units, control moves to one of them.

In every case the server pushes an unsolicited `SWITCH_CONTROL` to just that client so
its `controlled_entity_id` (used for the camera and the control indicator) stays correct.

---

## 7. Unit AI (`ai.go`)

`runAI` is called once per tick *before* `tick`, and is passed the set of entity IDs
currently controlled by a player so it can skip them. For each un-possessed unit:

- `nearestEnemy` = closest non-bullet entity with a different `OwnerID`.
- If one exists within `AIDetectionRange` → **attack**: steer straight at it and fire a
  bullet whenever `AttackCooldown` reaches 0.
- Otherwise → **follow**: move toward the owner's player entity until within
  `AIFollowRadius`, then stop.

There is no pathfinding — units move in straight lines. AI only ever sets `VelX/VelY`
and spawns bullets; the actual movement and collision still happen in `tick`, so AI
units obey exactly the same physics as players.

---

## 8. State synchronisation

**Strategy: full snapshot every tick, no acknowledgements.**

`buildGameState` encodes *all* entities into one `GAME_STATE` packet — 13 bytes each
(`id, type, owner_id, x, y, health`) — and `main.go` broadcasts it to every session at
20 Hz.

Consequences:
- **Loss-tolerant.** A dropped snapshot just means the client is one tick stale;
  the next one is complete. This is why UDP with no reliability layer is acceptable.
- **Authoritative and simple.** The client replaces its entire entity dictionary each
  packet (`_handle_game_state`), so there is no client-side reconciliation to get wrong.
- **Doesn't scale.** Bandwidth is `O(entities × clients)` every tick. Delta encoding is
  a roadmap item, and would need a reliability/ack mechanism to go with it.
- **No smoothing.** The client draws the latest snapshot as-is, so motion is quantised
  to the tick. Interpolation is not implemented.

Events that can't be recovered from a snapshot get their own packet: `ENTITY_DESTROYED`
(so the client can drop the entity immediately and, for bullets, not wait for it to
simply disappear from the next snapshot) and the per-client `SWITCH_CONTROL`.

---

## 9. Error handling

The server is written to **stay up**, not to fail fast:

- `ReadFromUDP` errors are logged and the loop continues.
- Every handler re-checks that the session / entity still exists after taking its lock,
  because a concurrent tick or leave may have removed it.
- Every handler bounds-checks its payload (`len(payload) < 8` etc.) and returns on a
  short packet rather than panicking.
- `WriteToUDP` return values are intentionally ignored — a client that can't be reached
  will be cleaned up by the (future) disconnect path; a failed send is not fatal.
- Unknown packet types fall through the dispatch `switch` and are dropped silently.

Known weak spots: logging is unstructured `fmt.Println`; there are no metrics; and the
`sender ID` byte is trusted rather than checked against the source address.

---

## 10. Client structure (`scene_1.gd`)

A single script on a single `Node2D`. Sections, in file order:

1. **Constants** — packet types, tile types, entity types, colours. These mirror the Go
   side and must stay in sync.
2. **State** — `udp`, `player_id`, `controlled_entity_id`, `seq`, the `entities`
   dictionary, the decoded map, and camera position.
3. **`_process`** — drain all pending packets, send input, update the camera.
4. **Input** — `_unhandled_input` for click-to-shoot (converts the mouse position to a
   world direction using the camera transform), `E`, and `F`.
5. **Send helpers** — `_make_header` (mirrors `makeHeader`) and one function per
   outbound packet.
6. **Receive** — `_handle_packet` dispatch and one handler per inbound packet.
   `_handle_game_state` walks the 13-byte records; `_handle_map_data` unpacks the grid.
7. **Camera** — `_update_camera`: per-axis dead-zone follow, each axis clamped to the
   map bounds independently (so a map smaller than the viewport on one axis stays
   centred there without an inverted clamp).
8. **Rendering** — `_draw` → `_draw_map` (ground fill + wall/structure rects) and
   `_draw_entities` (rects for players/units, circle for bullets, health bar, and a
   triangle marker over the entity this client controls).

---

## 11. Where to change things

| To change… | Edit |
|------------|------|
| Tick rate | `game-server/main.go` (`time.Second / 20`) |
| Listen port | `game-server/main.go` |
| Client target host/port | `game-client/multiplayer-game/scene_1.gd` `_ready()` |
| The map | `game-server/map.go` `mapLayout` (also update `MapWidth`/`MapHeight`) |
| Movement speed / health / bullet damage / TTL / cooldowns | `game-server/entity.go` |
| AI ranges / speed | `game-server/ai.go` |
| Add a packet type | `game-server/packets.go` **and** the constant block + `_handle_packet` in `scene_1.gd` |
| Change the `GAME_STATE` record layout | `buildGameState` in `game.go` **and** `_handle_game_state` in `scene_1.gd` (and `packets_test.go`) |
