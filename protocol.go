package dctoolkit

import (
	"compress/zlib"
	"github.com/direct-connect/go-dc/lineproto"
	"io"
	"net"
	"time"
)

const (
	_CONN_READ_TIMEOUT  = 60 * time.Second
	_CONN_WRITE_TIMEOUT = 10 * time.Second
	_MAX_MESSAGE_SIZE   = 10 * 1024
)

type msgDecodable interface{}
type msgEncodable interface{}

type monitoredConnIntf interface {
	PullReadCounter() uint
	PullWriteCounter() uint
}

type protocol interface {
	Close()
	SetSyncMode(val bool)
	SetReadBinary(val bool)
	Read() (msgDecodable, error)
	Write(msg msgEncodable)
	WriteSync(in []byte) error
	monitoredConnIntf
	ReaderActivateZlib() error
	WriterSetZlib(val bool)
}

// timedConn forces net.Conn to use timeouts.
type timedConn struct {
	io.Closer
	conn         net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func newTimedConn(conn net.Conn, readTimeout time.Duration,
	writeTimeout time.Duration) io.ReadWriteCloser {
	return &timedConn{
		Closer:       conn,
		conn:         conn,
		readTimeout:  readTimeout,
		writeTimeout: writeTimeout,
	}
}

func (c *timedConn) Read(buf []byte) (int, error) {
	if c.readTimeout > 0 {
		if err := c.conn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
			return 0, err
		}
	}
	return c.conn.Read(buf)
}

func (c *timedConn) Write(buf []byte) (int, error) {
	if c.writeTimeout > 0 {
		if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
			return 0, err
		}
	}
	return c.conn.Write(buf)
}

// monitoredConn implements a read and a writer counter, that provides the
// connection speed.
type monitoredConn struct {
	io.Closer
	in           io.ReadWriteCloser
	readCounter  uint
	writeCounter uint
}

func newMonitoredConn(in io.ReadWriteCloser) *monitoredConn {
	return &monitoredConn{
		Closer: in,
		in:     in,
	}
}

func (c *monitoredConn) Read(buf []byte) (int, error) {
	n, err := c.in.Read(buf)
	c.readCounter += uint(n)
	return n, err
}

func (c *monitoredConn) Write(buf []byte) (int, error) {
	n, err := c.in.Write(buf)
	c.writeCounter += uint(n)
	return n, err
}

func (c *monitoredConn) PullReadCounter() uint {
	ret := c.readCounter
	c.readCounter = 0
	return ret
}

func (c *monitoredConn) PullWriteCounter() uint {
	ret := c.writeCounter
	c.writeCounter = 0
	return ret
}

// zlibSwitchableWriter implements a write compression that can be switched
// on or off at any time.
type zlibSwitchableWriter struct {
	in           io.Writer
	zlibWriter   io.WriteCloser
	activeWriter io.Writer
}

func newZlibSwitchableWriter(in io.Writer) *zlibSwitchableWriter {
	return &zlibSwitchableWriter{
		in:           in,
		activeWriter: in,
	}
}

func (c *zlibSwitchableWriter) Write(buf []byte) (int, error) {
	return c.activeWriter.Write(buf)
}

func (c *zlibSwitchableWriter) SetZlib(val bool) {
	if (val && c.activeWriter == c.zlibWriter) ||
		(!val && c.activeWriter != c.zlibWriter) {
		return
	}

	if val == true {
		dolog(LevelDebug, "[write zlib on]")
		if c.zlibWriter == nil {
			c.zlibWriter = zlib.NewWriter(c.in)
		} else {
			c.zlibWriter.(*zlib.Writer).Reset(c.in)
		}
		c.activeWriter = c.zlibWriter
	} else {
		dolog(LevelDebug, "[write zlib off]")
		c.zlibWriter.Close()
		c.activeWriter = c.in
	}
}

type protocolBase struct {
	remoteLabel string
	msgDelim    byte
	sendChan    chan []byte
	terminated  bool
	closer      io.Closer
	monitoredConnIntf
	reader       *lineproto.Reader
	writer       *zlibSwitchableWriter
	readBinary   bool
	syncMode     bool
	writerJoined chan struct{}
}

func newProtocolBase(remoteLabel string, nconn net.Conn,
	applyReadTimeout bool, applyWriteTimeout bool, msgDelim byte) *protocolBase {

	readTimeout := func() time.Duration {
		if applyReadTimeout == true {
			return _CONN_READ_TIMEOUT
		}
		return 0
	}()
	writeTimeout := func() time.Duration {
		if applyWriteTimeout == true {
			return _CONN_WRITE_TIMEOUT
		}
		return 0
	}()

	tc := newTimedConn(nconn, readTimeout, writeTimeout)
	mc := newMonitoredConn(tc)
	rdr := lineproto.NewReader(mc, msgDelim)
	wri := newZlibSwitchableWriter(mc)

	p := &protocolBase{
		remoteLabel:       remoteLabel,
		msgDelim:          msgDelim,
		writerJoined:      make(chan struct{}),
		closer:            mc,
		monitoredConnIntf: mc,
		reader:            rdr,
		writer:            wri,
		sendChan:          make(chan []byte),
	}
	go p.writeReceiver()
	return p
}

func (p *protocolBase) Close() {
	if p.terminated == true {
		return
	}
	p.terminated = true
	p.closer.Close()

	if p.syncMode == false {
		close(p.sendChan)
		<-p.writerJoined
	}
}

func (p *protocolBase) SetSyncMode(val bool) {
	if val == p.syncMode {
		return
	}
	p.syncMode = val

	if val == true {
		close(p.sendChan)
		<-p.writerJoined

	} else {
		p.sendChan = make(chan []byte)
		go p.writeReceiver()
	}
}

func (p *protocolBase) SetReadBinary(val bool) {
	if val == p.readBinary {
		return
	}
	p.readBinary = val
}

func (p *protocolBase) ReadMessage() (string, error) {
	// Close() was called in a previous run
	if p.terminated == true {
		return "", errorTerminated
	}

	msg, err := p.reader.ReadLine()
	if err != nil {
		if p.terminated == true {
			return "", errorTerminated
		}
		return "", err
	}
	return string(msg[:len(msg)-1]), nil
}

func (p *protocolBase) ReadBinary() ([]byte, error) {
	// Close() was called in a previous run
	if p.terminated == true {
		return nil, errorTerminated
	}

	// TODO: move buf out or make static
	var buf [2048]byte
	read, err := p.reader.Read(buf[:])
	if read == 0 {
		if p.terminated == true {
			return nil, errorTerminated
		}
		return nil, err
	}
	return buf[:read], nil
}

func (p *protocolBase) writeReceiver() {
	for buf := range p.sendChan {
		// do not handle errors here
		p.WriteSync(buf)
	}
	p.writerJoined <- struct{}{}
}

func (p *protocolBase) WriteSync(in []byte) error {
	_, err := p.writer.Write(in)
	return err
}

func (p *protocolBase) Write(in []byte) {
	if p.terminated == true {
		return
	}
	p.sendChan <- in
}

func (p *protocolBase) ReaderActivateZlib() error {
	return p.reader.ActivateZlib()
}

func (p *protocolBase) WriterSetZlib(val bool) {
	p.writer.SetZlib(val)
}

type msgBinary struct {
	Content []byte
}
