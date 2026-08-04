package main

import (
	"fmt"
	"log"
	"net"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":1234")
	if err != nil {
		log.Fatal(err)
	}
	clientConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer clientConn.Close()

	fmt.Println("listening", addr.String())

	buffer := make([]byte, 2048)
	for {
		n, clientAddr, err := clientConn.ReadFromUDP(buffer)
		if err != nil {
			panic(err)
		}
		fmt.Println("read(client)", n, "bytes")
		fmt.Printf("Got message from %s: %s\n", clientAddr, string(buffer[:n]))

		n, err = clientConn.WriteToUDP([]byte("Yes, I am here!"), clientAddr)
		if err != nil {
			panic(err)
		}
		fmt.Println("wrote(client)", n, "bytes")
	}
}
