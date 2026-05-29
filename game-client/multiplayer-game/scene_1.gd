extends Node

var udp = PacketPeerUDP.new()

func _ready():
	# Connect to server
	udp.connect_to_host("127.0.0.1", 8080)

	print("Connected to server")

	# Send message
	var message = "hello server"
	udp.put_packet(message.to_utf8_buffer())

	print("Sent message to server")


func _process(delta):
	# Check if packets are available
	while udp.get_available_packet_count() > 0:
		
		# Receive packet
		var packet = udp.get_packet()
		
		# Convert bytes back to string
		var message = packet.get_string_from_utf8()
		
		print("Server says: ", message)
