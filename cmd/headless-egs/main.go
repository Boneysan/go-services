package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	fmt.Println("Headless EGS Smoke Test Prototype starting...")
	listener, err := net.Listen("tcp", ":3000")
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	fmt.Println("Listening on :3000 for mock EGS connections")
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Connection error:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Println("Received connection from", conn.RemoteAddr())
	conn.Write([]byte("WELCOME TO EGS HEADLESS"))
}
