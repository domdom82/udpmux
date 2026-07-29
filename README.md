# udpmux
udpmux encapsulates traffic with UDP. It is used to multiplex different flows over the same port.

## Architecture


### UDP Proxy
The UDP Proxy is responsible for receiving incoming UDP packets and wrapping them in a custom header that includes a destination endpoint.
The packets are forwarded to UDP Mux. Replies from the UDP Mux are received by the UDP Proxy, which extracts the inner packets and sends them back to the original sender.

### UDP Mux
The UDP Mux receives the encapsulated packets from the UDP Proxy, extracts the destination endpoint from the custom header, and forwards the inner packets to the appropriate destination.
Replies from the destination are sent back to the UDP Mux, which encapsulates them in the custom header and sends them back to the UDP Proxy.