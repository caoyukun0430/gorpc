package gorpc

// regulate the msg formatting that communicates, for now the way/type to encode/decode
// it's possible to have multi header and body in the same message
// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      JSON ENCODE      ------>  | <-------   CODING DECIDED BY CodeType   ------->|
// | Option | Header1 | Body1 | Header2 | Body2 | ...
import (
	"encoding/json"
	"errors"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int           // MagicNumber marks this's a gorpc request
	CodecType      codec.Type    // client may choose different Codec to encode body
	ConnectTimeout time.Duration // 0 means no limit, default will be 10s
	HandleTimeout  time.Duration // 0 means no limit
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10, // default connect timeout 10s
}

// implement server
// Server represents an RPC Server.
type Server struct {
	//  store key-value pairs and ensures thread-safe access to these pairs
	serviceMap sync.Map
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{}
}

// DefaultServer is the default instance of *Server.
var DefaultServer = NewServer()

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
// for loop wait for socket conn establishment
// handle the conn by go routine
func (server *Server) accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go server.ServeConn(conn)
	}
}

// Accept accepts connections on the listener and serves requests
// for each incoming connection.
func Accept(lis net.Listener) {
	DefaultServer.accept(lis)
}

// Register publishes in the server the set of methods
func (server *Server) register(rcvr interface{}) error {
	s := newService(rcvr)
	// loaded: A boolean indicating whether the key was already present.
	if _, loaded := server.serviceMap.LoadOrStore(s.name, s); loaded {
		return errors.New("rpc: service already defined: " + s.name)
	}
	return nil
}

// Register publishes the receiver's methods in the DefaultServer.
func Register(rcvr interface{}) error {
	return DefaultServer.register(rcvr)
}

// findService via serviceMethod name from the serverMap
func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request doesn't follow format Service.Method: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	serviceFound, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = serviceFound.(*service)
	mtype = svc.methodMap[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

// ServeConn runs the server on a single connection.
// ServeConn blocks, serving the connection until the client hangs up in serveCodec()
func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() { _ = conn.Close() }()
	var opt Option
	// Decode reads the next JSON-encoded value from its
	// input and stores it in the value pointed to by opt.
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	// NewCodecFuncMap[GobType] = NewGobCodec --> f
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	// when opt is received, send response to client to avoid opt being eaten issue https://github.com/geektutu/7days-golang/issues/26
	if err := json.NewEncoder(conn).Encode(&opt); err != nil {
		log.Println("rpc server: options sent error: ", err)
		return
	}
	server.serveCodec(f(conn), &opt)
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

// request stores all information of a client call
// reflect.Value allows you to dynamically inspect and manipulate values of any type.
type request struct {
	header *codec.Header // header of request
	// argv: Represents the arguments passed to the RPC method
	// replyv: Represents the value that will be returned by the RPC method.
	argv, replyv reflect.Value
	mtype        *methodType // use to init argv and replyv instance and (s *service) call(m *methodType, argv, replyv reflect.Value)
	svc          *service    // method receiver
}

// in one conn, multi request headers and body can be read and handled
// concurrently, handleRequest() is called with go rountine
// but response has to be sent serially with mutex lock so that client
// can decode it
// Codec return type from NewCodecFuncMap[GobType] = NewGobCodec
// three phase, read req, handle req, send resp
func (server *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex) // make sure to send a complete response
	wg := new(sync.WaitGroup)  // wait until all request are handled
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break // we do the best we can, when it's not possible to recover, close the connection
			}
			// .Error() returns a string?
			req.header.Error = err.Error()
			server.sendResponse(cc, req.header, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = cc.Close()
}

// now define methods called inside serveCodec
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var header codec.Header
	// read header info from conn and write into &header
	if err := cc.ReadHeader(&header); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &header, nil
}

func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{
		header: h,
	}
	// day3
	// req.mtype is req.svc.methodMap[methodName]
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	//create argv and replyv instance
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	// make sure that argvi is a pointer, ReadBody -> decode need a pointer as parameter, ReadBody to decode the data into the correct type.
	// The Decode method is responsible for deserializing the data from the connection into the body variable. To do this, it needs to modify the original value of body. If body were not a pointer, Decode would only modify a copy of the value, and the original body would remain unchanged.
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	// req.argv.Interface() returns the underlying value as an interface{}, which allows ReadBody to decode the data into the correct type.
	return req, nil
}

// func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
// 	// day3 func (s *service) call
// 	defer wg.Done()
// 	err := req.svc.call(req.mtype, req.argv, req.replyv)
// 	if err != nil {
// 		req.header.Error = err.Error()
// 		server.sendResponse(cc, req.header, invalidRequest, sending)
// 		return
// 	}
// 	server.sendResponse(cc, req.header, req.replyv.Interface(), sending)
// }

// Buffered Channels: By creating called and sent as buffered channels with a capacity of 1 (make(chan struct{}, 1)), you allow the goroutine to send to these channels without blocking, even if the main function has already timed out.
// Non-blocking Sends: The goroutine can send to the called and sent channels without waiting for a corresponding receive operation, thus preventing a goroutine leak.
// sent is independent from called!
// as long as called receives the signal, it means the handle hasnt timeout and we can continue to sendResponse!
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{}, 1)
	sent := make(chan struct{}, 1)
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.header.Error = err.Error()
			server.sendResponse(cc, req.header, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.header, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	// when this triggers, means called is timeout before received, so we need to send invalid response
	case <-time.After(timeout):
		req.header.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(cc, req.header, invalidRequest, sending)
	case <-called:
		<-sent
	}

}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	// write from conn buff to header and body
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
