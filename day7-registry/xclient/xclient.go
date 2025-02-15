package xclient

import (
	"context"
	. "gorpc" // With a dot import, you can use these identifiers directly without the gorpc prefix:
	"io"
	"reflect"
	"sync"
)

// we expose a client that support LB to user

type XClient struct {
	disc    Discovery // which Discovery instance
	mode    SelectMode
	opt     *Option
	mu      sync.Mutex // multi reader or single writer
	clients map[string]*Client
}

// Closer interface must have Close() error method
// ensure that the XClient type implements the io.Closer interface at compile time
var _ io.Closer = (*XClient)(nil)

// the Discovery instance, the LB mode and the option as arg
func NewXClient(d Discovery, mode SelectMode, opt *Option) *XClient {
	return &XClient{disc: d, mode: mode, opt: opt, clients: make(map[string]*Client)}
}

// Implement Closer interface
func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		// I have no idea how to deal with error, just ignore it.
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}

// check if client cached in clients[rpcAddr]
// if client is cached, check if client is healthy
// if not or noexist, create new client and cache it
func (xc *XClient) buildClient(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		// day5 XDial calls different functions to connect to a RPC server
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}
	return client, nil
}

func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, args, reply interface{}) error {
	client, err := xc.buildClient(rpcAddr)
	if err != nil {
		return err
	}
	return client.SyncCall(ctx, serviceMethod, args, reply)
}

// SyncCall invokes the named function SyncCall in client.go, waits for it to complete,
// and returns its error status.
// xc will choose a proper server.
func (xc *XClient) SyncCall(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.disc.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

// Broadcast invokes the named function for every server registered in discovery
// clonedReply allows each goroutine to work independently and safely. Once a successful response is received, it updates the original reply exactly once and continues until all calls are completed.
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.disc.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex // protect e and replyDone
	var e error
	replyDone := reply == nil // if reply is nil, don't need to set value
	ctx, cancel := context.WithCancel(ctx)
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(rpcAddr, ctx, serviceMethod, args, clonedReply)
			mu.Lock()
			// If any server call fails, it cancels the remaining calls and returns the first encountered error.
			if err != nil && e == nil {
				e = err
				cancel() // if any call failed, cancel unfinished calls
			}
			if err == nil && !replyDone {
				// copy succeed clonedReply to reply
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
