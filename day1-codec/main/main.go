package main

import (
	"encoding/json"
	"fmt"
	"gorpc"
	"gorpc/codec"
	"log"
	"net"
	"time"
)

// addr chan string declares a channel that can send and receive string values.
func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	// sends the address of the listener (which is a free port chosen by net.Listen("tcp", ":0")) to the addr channel. This allows the main function to receive the address and use it to establish a connection.
	addr <- l.Addr().String()
	gorpc.Accept(l)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	// in fact, following code is like a simple geerpc client
	conn, _ := net.Dial("tcp", <-addr)
	defer func() { _ = conn.Close() }()

	time.Sleep(time.Second)
	// encodes geerpc.DefaultOption into JSON format and sends it over the connection conn
	// json.NewEncoder(conn) function in Go creates a new json.Encoder that writes JSON-encoded data to the specified io.Writer, which in this case is the conn
	_ = json.NewEncoder(conn).Encode(gorpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	// send request & receive response
	for i := 0; i < 5; i++ {
		// A Header is created for each request, specifying the service method (Foo.Sum) and a sequence number (Seq).
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		//  cc.Write method is used to send the header and the request body (a formatted string) to the server.
		_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
		// receive response from server
		_ = cc.ReadHeader(h)
		// read and print body also gives the time for the requests to complete
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
