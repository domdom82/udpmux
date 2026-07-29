package main

import (
	"fmt"
	"net"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", "localhost:7070")
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		panic(err)
	}

	defer conn.Close()

	n, err := conn.Write([]byte("Hello are you there?"))
	if err != nil {
		panic(err)
	}

	fmt.Println("wrote(proxy)", n, "bytes")

	buffer := make([]byte, 1024)
	n, err = conn.Read(buffer)
	if err != nil {
		panic(err)
	}
	fmt.Println("read(proxy)", n, "bytes", n)

	fmt.Printf("Got message from %s: %s\n", addr, string(buffer[:n]))

}
