package main

import (
	"log"
	"net"

	"github.com/domdom82/udpmux/pkg/frame"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	clientConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer clientConn.Close()

	log.Println("udp-mux listening", addr.String())
	for {
		handleConn(clientConn)
	}

}

func handleConn(udpProxyConn *net.UDPConn) {
	defer udpProxyConn.Close()

	for {
		// Read from udp proxy side, send to endpoint side
		headerBytesIn := make([]byte, frame.HeaderV1Length)
		h, proxyAddr, err := udpProxyConn.ReadFromUDP(headerBytesIn)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(proxy)", h, "bytes header")

		frameIn, err := frame.Decode(headerBytesIn)
		if err != nil {
			log.Fatal(err)
		}

		payloadBytesIn := make([]byte, frameIn.Length)
		p, err := udpProxyConn.Read(payloadBytesIn)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(proxy)", p, "bytes payload")

		endpointStr := string(frameIn.Endpoint[:frameIn.EndpointLen])
		endPointConn, err := net.Dial("udp", endpointStr)
		if err != nil {
			log.Fatal(err)
		}
		defer endPointConn.Close()

		n2, err := endPointConn.Write(payloadBytesIn)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(endpoint)", n2, "bytes")

		// Read from endpoint side, send to udp proxy side
		buffer := make([]byte, 1024)
		n, err := endPointConn.Read(buffer)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("read(endpoint)", n, "bytes")

		frameOut, err := frame.NewHeader(endpointStr)
		if err != nil {
			log.Fatal(err)
		}
		frameOut.Length = uint16(n)
		headerBytesOut, err := frame.Encode(frameOut)
		if err != nil {
			log.Fatal(err)
		}

		h, err = udpProxyConn.WriteToUDP(headerBytesOut, proxyAddr)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(proxy)", h, "bytes header")

		payloadBytesOut := buffer[:n]
		p, err = udpProxyConn.WriteToUDP(payloadBytesOut, proxyAddr)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("wrote(proxy)", p, "bytes payload")
	}
}
