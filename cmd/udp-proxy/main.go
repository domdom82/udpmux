package main

import (
	"log"
	"net"

	"github.com/domdom82/udpmux/pkg/frame"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":7070")
	if err != nil {
		log.Fatal(err)
	}
	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer udpConn.Close()

	log.Println("udp-proxy listening", addr.String())
	for {
		handleConn(udpConn)
	}

}

func handleConn(clientConn *net.UDPConn) {
	defer clientConn.Close()

	udpMuxAddr, err := net.ResolveUDPAddr("udp", "localhost:8080")
	if err != nil {
		panic(err)
	}

	udpMuxConn, err := net.DialUDP("udp", nil, udpMuxAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer udpMuxConn.Close()

	for {
		// Read from client side, send to udp mux side
		buffer := make([]byte, 1024)
		n, clientAddr, err := clientConn.ReadFromUDP(buffer)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(client)", n, "bytes")

		frameOut, err := frame.NewHeader("localhost:1234")
		if err != nil {
			log.Fatal(err)
		}
		frameOut.Length = uint16(n)

		headerBytes, err := frame.Encode(frameOut)
		if err != nil {
			log.Fatal(err)
		}

		h, err := udpMuxConn.Write(headerBytes)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(mux)", h, "bytes header")

		p, err := udpMuxConn.Write(buffer[:n])
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(mux)", p, "bytes payload")

		// Read from udp mux side, send to client side
		headerBytesIn := make([]byte, frame.HeaderV1Length)
		h, err = udpMuxConn.Read(headerBytesIn)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(mux)", h, "bytes header")

		frameIn, err := frame.Decode(headerBytesIn)
		if err != nil {
			log.Fatal(err)
		}

		payloadBytesIn := make([]byte, frameIn.Length)
		p, err = udpMuxConn.Read(payloadBytesIn)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(mux)", p, "bytes payload")

		n2, err := clientConn.WriteToUDP(payloadBytesIn, clientAddr)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(client)", n2, "bytes")
	}
}
