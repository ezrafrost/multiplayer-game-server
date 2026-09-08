extends Node2D

const PKT_JOIN_REQ       = 0x01
const PKT_JOIN_ACK       = 0x02
const PKT_LEAVE          = 0x03
const PKT_PLAYER_INPUT   = 0x04
const PKT_GAME_STATE     = 0x05
const PKT_PING           = 0x06
const PKT_PONG           = 0x07
const PKT_CHAT_SEND      = 0x08
const PKT_CHAT_BROADCAST = 0x09
const PKT_SHOOT          = 0x0A
const PKT_MAP_DATA          = 0x0C
const PKT_ENTITY_DESTROYED  = 0x0D
const PKT_SPAWN_UNIT        = 0x0E
const PKT_SWITCH_CONTROL    = 0x0F

const TILE_EMPTY     = 0
const TILE_WALL      = 1
const TILE_STRUCTURE = 2
const TILE_SIZE      = 32

const ENTITY_PLAYER = 1
const ENTITY_UNIT   = 2
const ENTITY_BULLET = 3

const PLAYER_SIZE  := 20.0
const BULLET_RADIUS := 4.0
const COLOR_GROUND    := Color(0.18, 0.25, 0.12)
const COLOR_WALL      := Color(0.40, 0.40, 0.42)
const COLOR_STRUCTURE := Color(0.52, 0.38, 0.22)
const COLOR_SELF      := Color(0.2, 0.8, 0.2)
const COLOR_ENEMY     := Color(0.8, 0.2, 0.2)
const COLOR_BULLET    := Color(1.0, 0.9, 0.1)

var udp := PacketPeerUDP.new()
var player_id: int = 0
var controlled_entity_id: int = 0
var seq: int = 0

# entity_id -> {type, owner_id, pos, health}
var entities: Dictionary = {}

var map_tiles:  PackedByteArray = PackedByteArray()
var map_width:  int = 0
var map_height: int = 0

const CAMERA_MARGIN := 150.0  # dead-zone: camera only moves when player is within this many px of a screen edge
var camera_pos := Vector2.ZERO  # world-space position the camera is centred on

func _ready() -> void:
	udp.connect_to_host("127.0.0.1", 8080)
	_send_join_req()

func _process(_delta: float) -> void:
	while udp.get_available_packet_count() > 0:
		_handle_packet(udp.get_packet())
	if player_id > 0:
		_send_player_input()
	_update_camera()

func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventKey and event.pressed and not event.echo:
		if event.keycode == KEY_E and player_id > 0:
			udp.put_packet(_make_header(PKT_SPAWN_UNIT))
		if event.keycode == KEY_F and player_id > 0:
			udp.put_packet(_make_header(PKT_SWITCH_CONTROL))
	if event is InputEventMouseButton and event.button_index == MOUSE_BUTTON_LEFT and event.pressed:
		if player_id == 0 or not entities.has(controlled_entity_id):
			return
		var viewport_center := get_viewport_rect().size * 0.5
		var player_world    := entities[controlled_entity_id]["pos"] as Vector2
		var player_screen   := viewport_center + (player_world - camera_pos)
		var dir             := ((event as InputEventMouseButton).position - player_screen).normalized()
		send_shoot(dir)

# --- Sending ---

func _make_header(pkt_type: int) -> PackedByteArray:
	var h := PackedByteArray()
	h.resize(4)
	h[0] = pkt_type
	h[1] = player_id
	h.encode_u16(2, seq)
	seq = (seq + 1) & 0xFFFF
	return h

func _send_join_req() -> void:
	udp.put_packet(_make_header(PKT_JOIN_REQ))

func _send_player_input() -> void:
	var vel := Vector2.ZERO
	if Input.is_key_pressed(KEY_D): vel.x += 1.0
	if Input.is_key_pressed(KEY_A): vel.x -= 1.0
	if Input.is_key_pressed(KEY_S): vel.y += 1.0
	if Input.is_key_pressed(KEY_W): vel.y -= 1.0

	var pkt := _make_header(PKT_PLAYER_INPUT)
	pkt.resize(12)
	pkt.encode_float(4, vel.x)
	pkt.encode_float(8, vel.y)
	udp.put_packet(pkt)

func send_chat(message: String) -> void:
	var pkt := _make_header(PKT_CHAT_SEND)
	pkt.append_array(message.to_utf8_buffer())
	udp.put_packet(pkt)

func send_shoot(direction: Vector2) -> void:
	var pkt := _make_header(PKT_SHOOT)
	pkt.resize(12)
	pkt.encode_float(4, direction.x)
	pkt.encode_float(8, direction.y)
	udp.put_packet(pkt)

# --- Receiving ---

func _handle_packet(data: PackedByteArray) -> void:
	if data.size() < 4:
		return
	var pkt_type := data[0]
	var payload  := data.slice(4)

	match pkt_type:
		PKT_JOIN_ACK:
			# payload: [player_id][entity_id]
			player_id            = payload[0]
			controlled_entity_id = payload[1]
			print("Joined as player %d controlling entity %d" % [player_id, controlled_entity_id])
		PKT_GAME_STATE:
			_handle_game_state(payload)
		PKT_CHAT_BROADCAST:
			_handle_chat(payload)
		PKT_MAP_DATA:
			_handle_map_data(payload)
		PKT_ENTITY_DESTROYED:
			entities.erase(payload[0])
			queue_redraw()
		PKT_SWITCH_CONTROL:
			controlled_entity_id = payload[0]
			queue_redraw()
		PKT_PONG:
			pass

# Per entity: id(1) + type(1) + owner_id(1) + x(4) + y(4) + health(2) = 13 bytes
func _handle_game_state(payload: PackedByteArray) -> void:
	var updated := {}
	var offset  := 0
	while offset + 13 <= payload.size():
		var eid      := payload[offset]
		var etype    := payload[offset + 1]
		var owner_id := payload[offset + 2]
		var x        := payload.decode_float(offset + 3)
		var y        := payload.decode_float(offset + 7)
		var health   := payload.decode_s16(offset + 11)
		updated[eid] = {
			"type":     etype,
			"owner_id": owner_id,
			"pos":      Vector2(x, y),
			"health":   health,
		}
		offset += 13
	entities = updated
	queue_redraw()

func _handle_map_data(payload: PackedByteArray) -> void:
	if payload.size() < 2:
		return
	map_width  = payload[0]
	map_height = payload[1]
	map_tiles  = payload.slice(2)
	# Start camera centred on the map
	camera_pos = Vector2(map_width * TILE_SIZE, map_height * TILE_SIZE) * 0.5
	queue_redraw()

func _handle_chat(payload: PackedByteArray) -> void:
	if payload.size() < 1:
		return
	var sender_id := payload[0]
	var message   := payload.slice(1).get_string_from_utf8()
	print("Player %d: %s" % [sender_id, message])

func _update_camera() -> void:
	if map_tiles.is_empty():
		return
	var viewport_size := get_viewport_rect().size
	var map_size      := Vector2(map_width * TILE_SIZE, map_height * TILE_SIZE)
	var half_vp       := viewport_size * 0.5
	var has_player    := controlled_entity_id > 0 and entities.has(controlled_entity_id)
	var player_world  := (entities[controlled_entity_id]["pos"] as Vector2) if has_player else Vector2.ZERO

	# Handle each axis independently so a map that fits on one axis is always
	# centred on that axis, with no risk of an inverted clamp range causing jumps.
	if map_size.x <= viewport_size.x:
		camera_pos.x = map_size.x * 0.5
	elif has_player:
		var sx := half_vp.x + (player_world.x - camera_pos.x)
		if sx < CAMERA_MARGIN:
			camera_pos.x -= CAMERA_MARGIN - sx
		elif sx > viewport_size.x - CAMERA_MARGIN:
			camera_pos.x += sx - (viewport_size.x - CAMERA_MARGIN)
		camera_pos.x = clampf(camera_pos.x, half_vp.x, map_size.x - half_vp.x)

	if map_size.y <= viewport_size.y:
		camera_pos.y = map_size.y * 0.5
	elif has_player:
		var sy := half_vp.y + (player_world.y - camera_pos.y)
		if sy < CAMERA_MARGIN:
			camera_pos.y -= CAMERA_MARGIN - sy
		elif sy > viewport_size.y - CAMERA_MARGIN:
			camera_pos.y += sy - (viewport_size.y - CAMERA_MARGIN)
		camera_pos.y = clampf(camera_pos.y, half_vp.y, map_size.y - half_vp.y)

# --- Rendering ---

func _draw() -> void:
	var center := get_viewport_rect().size * 0.5
	_draw_map(center, camera_pos)
	_draw_entities(center, camera_pos)

func _draw_map(center: Vector2, origin: Vector2) -> void:
	if map_tiles.is_empty():
		return
	var tile_vec := Vector2(TILE_SIZE, TILE_SIZE)
	# Ground fill covering the map area
	var map_screen_tl := center + (Vector2.ZERO - origin)
	draw_rect(Rect2(map_screen_tl, Vector2(map_width * TILE_SIZE, map_height * TILE_SIZE)), COLOR_GROUND)
	# Draw walls and structures on top
	for ty in range(map_height):
		for tx in range(map_width):
			var tile := map_tiles[ty * map_width + tx]
			if tile == TILE_EMPTY:
				continue
			var world_pos  := Vector2(tx * TILE_SIZE, ty * TILE_SIZE)
			var screen_pos := center + (world_pos - origin)
			var color      := COLOR_WALL if tile == TILE_WALL else COLOR_STRUCTURE
			draw_rect(Rect2(screen_pos, tile_vec), color)

func _draw_entities(center: Vector2, origin: Vector2) -> void:
	for eid in entities:
		var e          := entities[eid] as Dictionary
		var screen_pos := center + ((e["pos"] as Vector2) - origin)
		match e["type"]:
			ENTITY_PLAYER, ENTITY_UNIT:
				var color := COLOR_SELF if e["owner_id"] == player_id else COLOR_ENEMY
				var half  := Vector2(PLAYER_SIZE, PLAYER_SIZE) * 0.5
				draw_rect(Rect2(screen_pos - half, Vector2(PLAYER_SIZE, PLAYER_SIZE)), color)
				_draw_health_bar(screen_pos, e["health"])
				if eid == controlled_entity_id:
					_draw_control_arrow(screen_pos)
			ENTITY_BULLET:
				draw_circle(screen_pos, BULLET_RADIUS, COLOR_BULLET)

func _draw_control_arrow(screen_pos: Vector2) -> void:
	# Downward-pointing triangle sitting above the health bar
	var tip    := screen_pos + Vector2(0.0,  -28.0)
	var base_l := screen_pos + Vector2(-7.0, -40.0)
	var base_r := screen_pos + Vector2( 7.0, -40.0)
	draw_colored_polygon(PackedVector2Array([tip, base_l, base_r]), Color.WHITE)

func _draw_health_bar(screen_pos: Vector2, health: int) -> void:
	const BAR_W    := 24.0
	const BAR_H    := 4.0
	const BAR_Y    := -18.0  # pixels above entity centre
	var bar_origin := screen_pos + Vector2(-BAR_W * 0.5, BAR_Y)
	draw_rect(Rect2(bar_origin, Vector2(BAR_W, BAR_H)), Color(0.2, 0.0, 0.0))
	var pct        := clampf(health / 100.0, 0.0, 1.0)
	if pct > 0.0:
		# Shifts green → red as health drops
		var bar_color := Color(1.0 - pct, pct, 0.05)
		draw_rect(Rect2(bar_origin, Vector2(BAR_W * pct, BAR_H)), bar_color)
