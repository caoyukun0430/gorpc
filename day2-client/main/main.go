package main

import (
	"fmt"
	"gorpc"
	"log"
	"net"
	"sync"
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
	client, _ := gorpc.Dial("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	// send request & receive response
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("gorpc req %d", i)
			var reply string
			if err := client.SyncCall("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}
