package codec

import "io"

// err = client.Call("Arith.Multiply", args, &reply)
// a typical request contains RPC service name.method name,  args
// so we put servicemethod, err into request header, but args into request body
// similarly reply will be in the response body

type Header struct {
	ServiceMethod string // format "Service.Method"
	Seq           uint64 // sequence number chosen by client
	Error         string
}

// codec are typically designed to be bidirectional, meaning they can be used by both the client and the server
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

// any function matching this signature can be assigned to a variable of type NewCodecFunc. Such functions will:
// Accept an io.ReadWriteCloser (an interface that combines io.Reader, io.Writer, and io.Closer).
// Return a Codec (an interface defined earlier)
// later new NewGobCodec() implements it
type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json" // not implemented
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
