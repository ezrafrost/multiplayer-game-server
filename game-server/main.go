package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":8080")
	if err != nil {
		fmt.Println("Failed to resolve address:", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Println("Failed to start server:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Server listening on :8080")

	worldMap := newMap()
	game := newGame(worldMap)
	server := newServer(conn, game, worldMap)

	// Fixed 20 Hz tick: AI sets intent → physics applies it → broadcast result
	go func() {
		ticker := time.NewTicker(time.Second / 20)
		for range ticker.C {
			game.runAI(server.controlledEntityIDs())
			pkts, deaths, destroyed := game.tick()
			for _, pkt := range pkts {
				server.broadcast(pkt)
			}
			for _, ownerID := range deaths {
				server.handlePlayerDeath(ownerID)
			}
			for _, entityID := range destroyed {
				server.handleEntityDestroyed(entityID)
			}
			server.broadcast(game.buildGameState())
		}
	}()

	buf := make([]byte, 1024)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Read error:", err)
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		go server.handle(data, clientAddr)
	}
}
