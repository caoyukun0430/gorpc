package codec

import (
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

// Gob is a binary encoding/decoding format provided by Go encoding/gob package.
// It is designed to efficiently encode and decode Go data structures, making it
// suitable for transmitting data between Go programs or storing data in a binary format

type GobCodec struct {
	// io.ReadWriteCloser is an interface that combines io.Reader, io.Writer, and io.Closer, making it suitable for network connections or other I/O streams that support reading, writing, and closing.
	conn io.ReadWriteCloser // conn is passed in from NewGobCodec
	buf  *bufio.Writer      // e buffer is used to accumulate encoded data before writing it to the underlying connection. This ensures that the data is written efficiently and reduces the likelihood of partial writes.
	dec  *gob.Decoder
	enc  *gob.Encoder
}

var _ Codec = (*GobCodec)(nil)

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	// creates a buffered writer that wraps the conn (which is an io.ReadWriteCloser). This buffered writer is assigned to the buf field of the GobCodec struct.
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf), //  ensures that the encoder writes to the buffer, later c.enc.Encode(h) encodes the header and writes it to the buffer, similar for body
	}
}

// for NewGobCodec to implement codeC interface, we need to implements methods:
// read header from conn and write into h
func (c *GobCodec) ReadHeader(h *Header) error {
	return c.dec.Decode(h)
}

// read body from conn and write into body
func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		// c.buf is a bufio.Writer that wraps a network connection, calling c.buf.Flush() will send all the buffered data over the network to the remote endpoint.
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	// encode are to the buffer enc:  gob.NewEncoder(buf)
	//  encodes the header and writes it to the buffer
	if err := c.enc.Encode(h); err != nil {
		log.Println("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		log.Println("rpc codec: gob error encoding body:", err)
		return err
	}
	return nil
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
