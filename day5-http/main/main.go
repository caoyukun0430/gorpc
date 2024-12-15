package main

import (
	"context"
	"gorpc"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// Foo service
type Foo int

type Args struct{ Num1, Num2 int }

// Foo.Sum Service.Method
func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	var foo Foo
	// DefaultServer.register
	if err := gorpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	// log.Println(" struct name: " + reflect.TypeOf(&foo).Name())
	// day5 different fixed port
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	gorpc.HandleHTTP()
	// day5 diff
	_ = http.Serve(l, nil)
	// gorpc.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)
	client, _ := gorpc.DialHTTP("tcp", <-addr)
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := client.SyncCall(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}
