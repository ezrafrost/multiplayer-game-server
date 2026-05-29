package main

import (
	"fmt"
	"net"
)

func main() {
	// Create UDP address
	addr, err := net.ResolveUDPAddr("udp", ":8080")
	if err != nil {
		fmt.Println("Failed to resolve address:", err)
		return
	}

	// Start listening for UDP packets
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Println("Failed to start server:", err)
		return
	}
	defer conn.Close()

	fmt.Println("UDP server listening on port 8080...")

	// Buffer to store incoming packets
	buffer := make([]byte, 1024)

	for {
		// Wait for packet
		n, clientAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			fmt.Println("Error reading packet:", err)
			continue
		}

		// Convert bytes to string
		message := string(buffer[:n])

		fmt.Printf("Received from %s: %s\n", clientAddr, message)

		// Reply to client
		response := "Hello client!"

		_, err = conn.WriteToUDP([]byte(response), clientAddr)
		if err != nil {
			fmt.Println("Error sending response:", err)
		}
	}
}
