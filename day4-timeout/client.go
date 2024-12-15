package gorpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// the purpose is to write a client support async and concurrent requests

// 1. go startServer(addr) starts the server, and the server starts listening to the port
// 2. geerpc.Dial("tcp", <-addr), the client creates a server on the corresponding port, and creates a corresponding client to call and receive the response
// 3. Dial will create a new client and pass options to the server, then create a corresponding Codec according to the configuration of options, and the client starts to call receive to receive information
// 4. In the for loop, the client calls call to send information. After call assembles the parameters through go, send sends the information. During the send process, the client sends the information to the server through write
// 5. After the server processes and returns the information in serveCodec, the client's receive coroutine will receive the information, and the reply will be read at this time. The corresponding call.Done receives the variable, which means the execution is over
// 6. After the execution is over, reply is passed to call.Reply, bound to the reply string in the main function, and the result is printed out.

// Call represents an active RPC.
type Call struct {
	Seq           uint64
	ServiceMethod string      // format "<service>.<method>"
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

// calling call.done() ensures that any goroutine waiting on the Done channel is notified that the RPC call has completed
// completedCall := <-call.Done  the operation will block until a value is available in the Done channel
func (call *Call) done() {
	call.Done <- call
}

// Client represents an RPC Client.
// There may be multiple outstanding Calls associated
// with a single Client, and a Client may be used by
// multiple goroutines simultaneously.
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex // exclusive lock to protect sequential req sending, avoid req head+body mixed up
	header   codec.Header
	mu       sync.Mutex // protect Client object
	seq      uint64
	pending  map[uint64]*Call // pending calls map, key is seq, val is Call object
	closed   bool             // user has called Close
	shutdown bool             // server has to be stopped due to failure
}

// force casting, need to implement Close()
var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

type clientResult struct {
	client *Client
	err    error
}

// newClientFunc represents any function that can take a network connection and an Option struct, and return a Client object along with an error.
// This type is useful for defining functions that initialize a new client, allowing for flexibility in how the client is created.
// IMPORTANT: the func can have any name, as long as the signature is same, it belong to type newClientFunc
type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

// Close the connection
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed {
		return ErrShutdown
	}
	client.closed = true
	return client.cc.Close()
}

// implement three methds for Call, registerCall, removeCall and terminateCall
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// removeCall is called when Call is finished processing in send req or receiving resp
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// when error occurs during receiving resps, so terminateCalls pending calls
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// we need to init Client object, 1st step is to exchange Option protocol with server
// after that create an go routine receive() to continously receiving resps
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client init error:", err)
		return nil, err
	}
	// send options with server, same as day1 main.go
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options sending error: ", err)
		_ = conn.Close()
		return nil, err
	}
	// after client send opt, verify opt ACK is received too!
	if err := json.NewDecoder(conn).Decode(opt); err != nil {
		log.Println("rpc client: options received error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		seq:     1, // seq starts with 1, 0 means invalid call
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		// remove after client once received and processed
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			// it usually means that Write partially failed
			// and call was already removed.
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	// error occurs, so terminateCalls pending calls
	client.terminateCalls(err)
}

// timeout for client during connection creation, we wrap dial by timeout
// Same as day1, use Dial connects to an RPC server at the specified network address
// func Dial(network, address string, opts ...*Option) (client *Client, err error) {
// 	opt, err := parseOptions(opts...)
// 	if err != nil {
// 		return nil, err
// 	}
// 	conn, err := net.Dial(network, address)
// 	if err != nil {
// 		return nil, err
// 	}
// 	// close the connection if client is nil
// 	defer func() {
// 		if client == nil {
// 			_ = conn.Close()
// 		}
// 	}()
// 	return NewClient(conn, opt)
// }

// day4 wrap Dial with timeout, utilize net.DialTimeout
func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	// close the connection if client is nil
	defer func() {
		if client == nil {
			_ = conn.Close()
		}
	}()
	// make a chan to receive NewClient(conn, opt) results so that we can use select to control timeout
	ch := make(chan clientResult)
	go func() {
		// NewClient(conn, opt)
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()
	// in case there is no limit in connect timeout, we endless waiting
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

func Dial(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewClient, network, address, opts...)
}

func parseOptions(opts ...*Option) (*Option, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

// besides the receive, client also need send
func (client *Client) send(call *Call) {
	// make sure that the client will send a complete request
	client.sending.Lock()
	defer client.sending.Unlock()

	// we need to register the call in client to update the seq
	// then form the header and body, which is the call.Args
	// register this call.
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	// prepare request header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	// encode and send the request
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		// if we have err sending, we need to remove the call failed to be sent
		failedCall := client.removeCall(seq)
		// call may be nil, it usually means that Write partially failed,
		// client has received the response and handled
		if failedCall != nil {
			failedCall.Error = err
			failedCall.done()
		}
	}
}

// TWO methods exposed to user
// SendCall triggers async call sent
func (client *Client) SendCall(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	// done needs to be a buffer channel
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	// the call return doesnt need to wait to be sent!
	go client.send(call)
	return call
}

// SyncCall wraps around SendCall to be sync blocked until done is received.
// if we want it unblock we can also do

// call := client.SendCall( ... )
// # start new go routine to async non-block waiting
// go func(call *Call) {
// 	select {
// 		<-call.Done:
// 			# do something
// 		<-otherChan:
// 			# do something
// 	}
// }(call)

// otherFunc() # non-block other func execution

// need to wrap the syncCall with timeout, using context so that user can flexibly decide to use context.WithTimeout
// func (client *Client) SyncCall(serviceMethod string, args, reply interface{}) error {
// 	call := <-client.SendCall(serviceMethod, args, reply, make(chan *Call, 1)).Done
// 	return call.Error
// }

func (client *Client) SyncCall(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.SendCall(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	// Call struct includes a Done channel of type chan *Call. This means the channel can carry pointers to Call structs.
	case result := <-call.Done:
		return result.Error
	}
}
