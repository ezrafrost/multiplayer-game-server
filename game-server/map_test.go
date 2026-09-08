package main

import "testing"

// The map is the server's collision authority, so these tests pin down the two
// properties the physics step in tick() relies on: the grid matches the authored
// layout, and anything outside the grid reads as solid.

func TestNewMapMatchesLayout(t *testing.T) {
	m := newMap()
	if m.Width != MapWidth || m.Height != MapHeight {
		t.Fatalf("map is %dx%d, want %dx%d", m.Width, m.Height, MapWidth, MapHeight)
	}
	for y := 0; y < MapHeight; y++ {
		for x := 0; x < MapWidth; x++ {
			if got := m.tileAt(x, y); got != mapLayout[y][x] {
				t.Errorf("tileAt(%d,%d) = %d, want %d", x, y, got, mapLayout[y][x])
			}
		}
	}
}

func TestTileAtOutOfBoundsIsWall(t *testing.T) {
	m := newMap()
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {MapWidth, 0}, {0, MapHeight}} {
		if got := m.tileAt(c[0], c[1]); got != TileWall {
			t.Errorf("tileAt(%d,%d) = %d, want TileWall so entities can't leave the map", c[0], c[1], got)
		}
	}
}

func TestSolidAtClassifiesTiles(t *testing.T) {
	m := newMap()
	// mapLayout: (1,1) is open, (0,0) is border wall, (2,2) is a structure.
	if m.solidAt(1.5*TileSize, 1.5*TileSize) {
		t.Error("open tile (1,1) should not be solid")
	}
	if !m.solidAt(0.5*TileSize, 0.5*TileSize) {
		t.Error("border wall tile (0,0) should be solid")
	}
	if !m.solidAt(2.5*TileSize, 2.5*TileSize) {
		t.Error("structure tile (2,2) should be solid")
	}
}

func TestSolidAABBDetectsAdjacentWall(t *testing.T) {
	m := newMap()
	if m.solidAABB(1.5*TileSize, 1.5*TileSize, EntityRadius) {
		t.Error("AABB centred in an open tile should not be solid")
	}
	// Nudged west until the box overlaps the x=0 border wall in tile column 0.
	if !m.solidAABB(TileSize+EntityRadius-1, 1.5*TileSize, EntityRadius) {
		t.Error("AABB overlapping the west wall should be solid")
	}
}

func TestMapToBytesRoundTrip(t *testing.T) {
	m := newMap()
	b := m.ToBytes()
	if len(b) != 2+MapWidth*MapHeight {
		t.Fatalf("serialised length = %d, want %d", len(b), 2+MapWidth*MapHeight)
	}
	if int(b[0]) != MapWidth || int(b[1]) != MapHeight {
		t.Fatalf("serialised dimensions = %dx%d, want %dx%d", b[0], b[1], MapWidth, MapHeight)
	}
	for i, tile := range m.Tiles {
		if b[2+i] != tile {
			t.Fatalf("tile %d = %d, want %d", i, b[2+i], tile)
		}
	}
}
