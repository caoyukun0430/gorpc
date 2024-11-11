package gorpc

// regulate the msg formatting that communicates, for now the way/type to encode/decode
// it's possible to have multi header and body in the same message
// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      JSON ENCODE      ------>  | <-------   CODING DECIDED BY CodeType   ------->|
// | Option | Header1 | Body1 | Header2 | Body2 | ...
import (
	"encoding/json"
	"fmt"
	"gorpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber int        // MagicNumber marks this's a gorpc request
	CodecType   codec.Type // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}

// implement server
// Server represents an RPC Server.
type Server struct{}

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
	server.serveCodec(f(conn))
}

// invalidRequest is a placeholder for response argv when error occurs
var invalidRequest = struct{}{}

// request stores all information of a call
// reflect.Value allows you to dynamically inspect and manipulate values of any type.
type request struct {
	header *codec.Header // header of request
	// argv: Represents the arguments passed to the RPC method
	// replyv: Represents the value that will be returned by the RPC method.
	argv, replyv reflect.Value
}

// in one conn, multi request headers and body can be read and handled
// concurrently, handleRequest() is called with go rountine
// but response has to be sent serially with mutex lock so that client
// can decode it
// Codec return type from NewCodecFuncMap[GobType] = NewGobCodec
// three phase, read req, handle req, send resp
func (server *Server) serveCodec(cc codec.Codec) {
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
		go server.handleRequest(cc, req, sending, wg)
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
	// TODO: now we don't know the type of request argv
	// day 1, just suppose it's string
	// req.argv is set to a new reflect.Value of type string
	req.argv = reflect.New(reflect.TypeOf(""))
	// req.argv.Interface() returns the underlying value as an interface{}, which allows ReadBody to decode the data into the correct type.
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv err:", err)
	}
	return req, nil
}

func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	// TODO, should call registered rpc methods to get the right replyv
	// day 1, just print argv and send a hello message
	defer wg.Done()
	// reg.argv which is the body is written in cc.ReadBody()
	log.Println("header:", req.header, "body:", req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("gorpc resp %d", req.header.Seq))
	server.sendResponse(cc, req.header, req.replyv.Interface(), sending)
}

func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	// write from conn buff to header and body
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
