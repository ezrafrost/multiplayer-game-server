# Multiplayer Game Server

![Status](https://img.shields.io/badge/status-in%20development-orange)
![Server](https://img.shields.io/badge/server-Go%201.26-00ADD8)
![Client](https://img.shields.io/badge/client-Godot%204.6-478CBF)
![Transport](https://img.shields.io/badge/transport-UDP-lightgrey)

A top-down 2D multiplayer arena game built around a **server-authoritative** architecture:
a **Go** server owns the entire simulation, and a **Godot 4** client renders state and
sends player intent. The two communicate over a hand-rolled binary protocol on **UDP**.

> **Status: 🚧 In development.** The full loop is implemented — join, server-authoritative
> movement, shooting, AI unit spawning, control switching, and real-time state broadcast
> to every client — and was built against the running server. This is an active learning
> project: there are no menus, no client-side prediction, a single hard-coded map, and
> connection details are hard-coded. See [Development Status](#development-status) and
> [Roadmap](#roadmap) for exactly what is and isn't done.

---

## Overview

The project is split into two independent programs:

| Component | Language | Role |
|-----------|----------|------|
| `game-server/` | Go (stdlib only) | Authoritative game simulation, networking, session management |
| `game-client/multiplayer-game/` | Godot 4.6 / GDScript | Input capture, rendering, camera |

The design goal is that **the client trusts nothing it computes itself**. It sends
inputs ("I am holding W", "I clicked in this direction") and renders whatever entity
snapshot the server sends back. All movement, collision, damage, death, AI, and map
collision happen on the server.

### The Go server is responsible for

- Accepting UDP connections and assigning player IDs (session lifecycle)
- Running a **fixed 20 Hz simulation tick** independent of packet arrival
- The entity model — players, AI units, and bullets share one `Entity` type
- Physics: velocity integration, tile-grid collision with wall sliding, entity–entity separation
- Combat: bullet travel, lifetime, hit detection, damage, death and respawn
- Server-side unit AI (nearest-enemy targeting, follow / attack behaviour)
- The "possession" model — a player controls one entity at a time and can switch
- Owning the map and sending it to clients on join
- Broadcasting a full world snapshot every tick

### The Godot client is responsible for

- Opening the UDP socket and sending a join request
- Sampling keyboard/mouse input and sending it each frame
- Decoding snapshot packets into a local entity dictionary
- Immediate-mode rendering (`_draw`) of the map, entities, health bars, and the
  "you control this" indicator
- A dead-zone follow camera clamped to the map bounds

---

## Architecture

```
┌──────────────────────────┐         UDP :8080          ┌───────────────────────────────┐
│      Godot 4 client      │   custom binary protocol   │          Go server            │
│                          │ ─────────────────────────► │                               │
│  • sample WASD / mouse   │   JOIN / INPUT / SHOOT /    │  net.ListenUDP  (1 goroutine) │
│  • send player intent    │   SPAWN_UNIT / SWITCH      │        │                       │
│                          │                            │        ▼  go handle(pkt)      │
│  • decode GAME_STATE     │ ◄───────────────────────── │   packet handlers set intent  │
│  • _draw() the world     │   JOIN_ACK / MAP_DATA /    │        │  (velocity, cooldown) │
│                          │   GAME_STATE / DESTROYED   │        ▼                       │
└──────────────────────────┘                            │   20 Hz tick goroutine:       │
                                                        │   runAI → tick() → broadcast  │
                                                        │        │                      │
                                                        │        ▼                      │
                                                        │  entities map (sync.Mutex)    │
                                                        │  tile map (collision auth)    │
                                                        └───────────────────────────────┘
```

The single most important rule: **packet handlers never move anything.** They only
record intent (a velocity, a "wants to shoot" with a cooldown, a new entity). The next
simulation tick reads that intent and produces the authoritative result.

A deeper write-up — connection lifecycle, message flow, concurrency model, server
authority, and known limitations — is in **[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)**.

---

## Technologies

- **Go 1.26** — server. Standard library only (`net`, `encoding/binary`, `sync`,
  `math`, `time`); no external dependencies.
- **Godot 4.6** / **GDScript** — client, using `PacketPeerUDP` and immediate-mode `_draw()`.
- **UDP** with a custom binary protocol — no game framework, no RPC layer.

---

## Current Features

Implemented and exercised during development (see
[Development Status](#development-status) for what has and hasn't been independently verified):

- **Session management** — join request → assigned player ID + spawned player entity;
  server-side leave handling
- **Server-authoritative movement** — client sends a direction vector, server applies
  speed, integrates position, and resolves tile collisions with axis-separated sliding
- **Entity system** — one `Entity` type covering players, units, and bullets
- **Combat** — click to fire a bullet entity; server simulates travel, TTL, wall/entity
  hits, applies damage, destroys entities or respawns players at 0 HP
- **Server-side unit AI** — spawned units follow their owner and switch to a
  move-and-shoot attack behaviour when an enemy is in range
- **Possession / control switching** — one player drives one entity at a time; `F`
  cycles owned entities, and control is reassigned automatically when the controlled
  entity dies
- **Map streaming** — the server's tile grid is serialised and sent to each client on join
- **Full-snapshot netcode** — every tick the server broadcasts all entities (id, type,
  owner, position, health)
- **Follow camera** — dead-zone camera that only scrolls near screen edges and clamps
  to the map bounds
- **Chat** — send/broadcast packets exist and work; **no UI yet** (messages print to
  the client console)

---

## Getting Started

### Prerequisites

- **Go 1.26+** (`go version`) — [go.dev/dl](https://go.dev/dl/)
- **Godot 4.6+** (standard build, GDScript — the Mono/.NET build is not required) —
  [godotengine.org](https://godotengine.org/download)

### Running the Server

```bash
cd game-server
go run .
```

You should see:

```
Server listening on :8080
```

The server binds UDP port **8080** on all interfaces. To build a standalone binary
instead:

```bash
cd game-server
go build -o game-server        # game-server.exe on Windows
./game-server
```

Run the tests:

```bash
cd game-server
go test ./...
```

### Running the Godot Client

1. Open **Godot 4.6**, import `game-client/multiplayer-game/project.godot`.
2. Press **F5** (Run Project). The client immediately connects to `127.0.0.1:8080`
   and sends a join request.
3. For a multiplayer test, run more than one client — in the Godot editor,
   **Debug → Run Multiple Instances → 2+**, or launch several editor/export copies.
   The server must be started first.

**Controls**

| Input | Action |
|-------|--------|
| `W` `A` `S` `D` | Move the controlled entity |
| Mouse left-click | Shoot toward the cursor |
| `E` | Spawn an AI unit you own |
| `F` | Switch control to your next owned entity |

### Configuration

There is **no configuration file or environment variable support yet** (it's on the
[roadmap](#roadmap)). The values you are most likely to change are hard-coded:

| Setting | Location |
|---------|----------|
| Server listen address (`:8080`) | `game-server/main.go` |
| Client target address (`127.0.0.1:8080`) | `game-client/multiplayer-game/scene_1.gd` → `_ready()` |
| Tick rate (20 Hz) | `game-server/main.go` |
| Map layout | `game-server/map.go` → `mapLayout` |
| Speeds / health / cooldowns / bullet TTL | `game-server/entity.go` |
| AI ranges and speed | `game-server/ai.go` |

---

## Project Structure

```
multiplayer-game-server/
├── game-server/                 # Go — authoritative server (package main)
│   ├── main.go                  # entry point: UDP listener + 20 Hz tick goroutine
│   ├── server.go                # sessions, packet dispatch, control reassignment
│   ├── game.go                  # entity store, the tick() simulation step, snapshot encoding
│   ├── entity.go                # Entity struct + tuning constants
│   ├── ai.go                    # per-unit AI: nearest-enemy, follow / attack behaviour
│   ├── map.go                   # tile grid, collision queries, map serialisation
│   ├── packets.go               # packet-type constants + encode/decode helpers
│   ├── map_test.go              # collision / serialisation tests
│   ├── packets_test.go          # wire-format tests
│   └── go.mod
│
├── game-client/
│   └── multiplayer-game/        # Godot 4.6 project
│       ├── project.godot
│       ├── scene1.tscn          # single scene: one Node2D
│       ├── scene_1.gd           # all client logic: net, input, camera, rendering
│       └── icon.svg
│
├── shared/
│   └── MAPS.txt                 # scratch pad of hand-drawn map layouts (design notes, not code)
│
├── docs/
│   └── ARCHITECTURE.md          # detailed architecture / protocol reference
│
├── CLAUDE.md                    # repo orientation notes
└── README.md
```

---

## Networking

**Transport:** UDP. No reliability, ordering, or handshake layer yet — the game loop
is designed to tolerate loss because every tick carries a full snapshot.

**Packet header (4 bytes), every packet:**

| Offset | Size | Field | Notes |
|-------:|-----:|-------|-------|
| 0 | 1 | packet type | see table below |
| 1 | 1 | sender ID | player ID; `0` from the server |
| 2 | 2 | sequence | little-endian; client increments, currently unused server-side |

Multi-byte numbers are **little-endian**; positions are `float32`.

**Packet types**

| Value | Name | Dir | Payload |
|------:|------|:---:|---------|
| `0x01` | `JOIN_REQ` | C→S | — |
| `0x02` | `JOIN_ACK` | S→C | `player_id (1)`, `entity_id (1)` |
| `0x03` | `LEAVE` | C→S | — *(handled by server; client does not send it yet)* |
| `0x04` | `PLAYER_INPUT` | C→S | `dir_x (f32)`, `dir_y (f32)` — unit vector |
| `0x05` | `GAME_STATE` | S→C | repeated 13-byte entity records (below) |
| `0x06` / `0x07` | `PING` / `PONG` | C↔S | — |
| `0x08` / `0x09` | `CHAT_SEND` / `CHAT_BROADCAST` | C↔S | UTF-8 bytes (`CHAT_BROADCAST` prefixes `sender_id`) |
| `0x0A` | `SHOOT` | C→S | `dir_x (f32)`, `dir_y (f32)` |
| `0x0C` | `MAP_DATA` | S→C | `width (1)`, `height (1)`, `width*height` tile bytes |
| `0x0D` | `ENTITY_DESTROYED` | S→C | `entity_id (1)` |
| `0x0E` | `SPAWN_UNIT` | C→S | — |
| `0x0F` | `SWITCH_CONTROL` | C↔S | C→S: none (cycle). S→C: `entity_id (1)` now controlled |

**`GAME_STATE` entity record (13 bytes each):**

```
id (u8) | type (u8) | owner_id (u8) | x (f32) | y (f32) | health (i16)
```

`type` is `1 = player`, `2 = unit`, `3 = bullet`. Tile bytes are
`0 = empty`, `1 = wall`, `2 = structure`.

The client re-implements this layout by hand in GDScript, so the packet constants and
the 13-byte record are a contract the Go tests (`packets_test.go`) exist to protect.

---

## Development Status

**What has been verified**

- The Go server **builds** with Go 1.26 and passes `go vet` and `go test ./...`.
- The server **binds UDP :8080** and runs its tick loop.
- The client script implements the full protocol above and was developed against the
  running server.

**What has *not* been independently verified in this pass**

- An end-to-end play session was not re-run inside this environment (no GUI/Godot
  available here). The client/server integration is the author's working state, not a
  fresh CI result.

**Known limitations / rough edges (by design, for now)**

- Single hard-coded map; all players spawn at the same point
- No client-side prediction or interpolation — the client renders the last snapshot
  directly, so movement is only as smooth as the 20 Hz tick
- No menus, lobby, or loading screen — the client joins on launch
- The client never sends `LEAVE`; a player who closes the window lingers server-side
  until the process is restarted (no timeout/keepalive yet)
- `sender ID` in packets is trusted, not validated against the source address
- Chat has no UI (console only)
- Connection details and the map are hard-coded (no config file / env vars)
- `game-server.exe` was previously committed; it is now git-ignored

---

## Roadmap

### ✅ Completed

- Server-side simulation with a fixed tick, replacing raw client-driven movement
- Generic `Entity` model (players / units / bullets)
- Multi-file server package split
- Tile map with wall + structure collision, owned by the server, streamed to clients
- Combat: server-side bullets, hit detection, damage, death, respawn
- `SPAWN_UNIT` and server-owned units that fight
- Unit AI: idle/follow → move-toward-enemy → attack state behaviour
- `SWITCH_CONTROL` possession model with automatic reassignment on death
- Client rendering of map, entities, health bars, control indicator, follow camera
- Focused unit tests for collision and wire format

### 🚧 In Progress

- Stabilising the client-side combat/camera integration (uncommitted working-tree changes)

### 🗓️ Planned

- Join / leave flow with a loading screen and a real disconnect path (client `LEAVE`
  + server-side timeout/keepalive)
- Varied spawn points with "don't spawn in a wall / on another entity" checks
- Multiple unit types — differing size, speed, health, and bullet behaviour
  (fast / heavy / splash / homing rounds)
- Client-side interpolation (and possibly prediction) for smooth movement between ticks
- Config for host/port/tick rate (flags or a config file) instead of hard-coded values
- Delta-encoded snapshots instead of full-state broadcast
- Chat UI and a basic HUD (unit count, health)
- Map/tileset art pass — grass, dirt, trees, rocks, buildings; player and vehicle sprites

---

## Technical Challenges / Engineering Decisions

- **Server authority.** The earliest version just accumulated client-sent velocity with
  no simulation. Everything since has been about moving trust to the server: the client
  now sends only intent, and the server is the single source of truth for position,
  health, and death. This is the change the rest of the design depends on.

- **Fixed timestep, decoupled from I/O.** Network reads happen on the main goroutine;
  the simulation advances on a separate `time.Ticker` at 20 Hz. Inbound packets mutate
  intent; the tick turns intent into results. This keeps simulation step size stable
  regardless of packet timing or burst load.

- **Concurrency model.** Each inbound packet is handled on its own goroutine. Two
  mutexes guard the two pieces of shared state — one for the session table, one for the
  entity store — and are deliberately taken in separate critical sections. A
  `…Locked` naming convention marks helpers that assume the caller already holds the
  entity lock, to keep the locking discipline visible in the code.

- **Collision that feels right.** Movement is resolved one axis at a time so an entity
  sliding along a wall keeps its parallel velocity instead of stopping dead. A separate
  pass pushes overlapping entities apart so players and units can't stack on one pixel.

- **Possession instead of one-player-one-avatar.** A player has a "currently controlled
  entity" rather than a fixed avatar. Input, shooting, and unit spawning all act on
  whatever that is, and when it dies the server picks a replacement and tells just that
  client. This made unit control and respawn fall out of the same mechanism.

- **Hand-rolled UDP protocol.** No game-networking library. UDP avoids head-of-line
  blocking, and a compact fixed-layout binary format keeps snapshots small. The
  trade-off — no reliability or ordering — is currently absorbed by sending full state
  every tick; delta encoding will need a reliability story to go with it.

---

## Future Improvements

Beyond the roadmap features, areas the author is aware need work:

- Replace `fmt.Println` logging with structured, levelled logging
- Input validation on the `sender ID` field (bind it to the source `UDPAddr`)
- Bounded/pooled packet buffers instead of a fresh allocation per inbound packet
- Integration tests that stand up the server and drive it through a real join/move/shoot
  sequence
- A headless test client to allow CI to exercise the full loop
