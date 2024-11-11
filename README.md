# 7 Days Go RPC from Scratch

Go RPC studies and simplifies the [net/rpc](https://pkg.go.dev/net/rpc) implementation, and extends with the features:
* protocol exchange
* registry
* service discovery
* load balancing
* timeout processing

## Day 1 - Encode/decode and RPC server implementation

What we learnt?

1. Implment our own Goc codec instance based on Go encoding/gob package, which is designed to efficiently encode
and decode Go data structures in binary format. Our codec has mainly four basic methods, readHeader, readBody
write and close.

2. Regulate the communication message format, currently the coding type is only thing needs to be negoiate
in the message. We regulate the message heads begins with the coding option, followed by multiple requests
in one connection.

3. The way messages are handled in the connection/codec are three steps: first, read the request header and
body from the connection. A endless loop is used to continously read requests until the client closes.
Second, handle request and send responses. Note, response should be sent with exclusive lock because
we need to make sure response won't mix up in the messages so that the peer client can decode.

```go
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
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}
}
```


## Day 2 - Asynchronous and Concurrent Client Implementation

What we learnt?

1. go startServer(addr) starts the server, and the server starts listening to the port

2. geerpc.Dial("tcp", <-addr), the client creates a server on the corresponding port, and creates a corresponding client to call and receive the response

3. Dial will create a new client and pass options to the server, then create a corresponding Codec according to the configuration of options, and the client starts to call receive to receive information async.

4. In the for loop, the client calls SyncCall to send information. After call assembles the parameters through go, send sends the information. During the send process, the client sends the information to the server through write func

5. After the server processes and returns the information in serveCodec, the client's receive go routine will receive the information, and the reply will be read at this time. The corresponding call.Done receives the variable, which means the execution is over

6. After the execution is over, reply is passed to call.Reply, bound to the reply string in the main function, and the result is printed out.