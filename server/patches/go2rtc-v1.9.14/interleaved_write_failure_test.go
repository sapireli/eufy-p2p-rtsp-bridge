package rtsp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/tcp"
	"github.com/pion/rtp"
)

type prefixFailureConn struct {
	wire    bytes.Buffer
	calls   int
	closed  bool
	prefix  int
	failure error
}

func (c *prefixFailureConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *prefixFailureConn) Write(p []byte) (int, error) {
	if c.closed {
		return 0, net.ErrClosed
	}
	c.calls++
	if c.calls == 1 {
		c.wire.Write(p[:c.prefix])
		return c.prefix, c.failure
	}
	return c.wire.Write(p)
}
func (c *prefixFailureConn) Close() error { c.closed = true; return nil }
func (c *prefixFailureConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8554}
}
func (c *prefixFailureConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 12345}
}
func (c *prefixFailureConn) SetDeadline(time.Time) error      { return nil }
func (c *prefixFailureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *prefixFailureConn) SetWriteDeadline(time.Time) error { return nil }

func syntheticHandler(c *Conn) core.HandlerFunc {
	return c.packetWriter(&core.Codec{Name: core.CodecH264, ClockRate: 90000, PayloadType: 96}, 0, 96)
}
func syntheticFrame(seq uint16) *rtp.Packet {
	return &rtp.Packet{Header: rtp.Header{Version: 2, Marker: true, SequenceNumber: seq, Timestamp: uint32(seq) * 6000, SSRC: 1}, Payload: bytes.Repeat([]byte{byte(seq)}, 20)}
}
func TestInterleavedPartialFailureClosesBeforeNextFrame(t *testing.T) {
	for _, failure := range []error{io.ErrUnexpectedEOF, nil} {
		t.Run("prefix", func(t *testing.T) {
			wire := &prefixFailureConn{prefix: 7, failure: failure}
			c := &Conn{conn: wire, state: StatePlay, playOK: true, OnClose: func() error { t.Fatal("consumer cleanup called inside sender"); return nil }}
			handler := syntheticHandler(c)
			handler(syntheticFrame(1))
			handler(syntheticFrame(2))
			if !wire.closed || wire.calls != 1 || wire.wire.Len() != 7 {
				t.Fatalf("unsafe continued framing: closed=%v calls=%d wireBytes=%d firstDeclaredRTP=%d", wire.closed, wire.calls, wire.wire.Len(), uint16(wire.wire.Bytes()[2])<<8|uint16(wire.wire.Bytes()[3]))
			}
		})
	}
}
func TestInterleavedCompleteWriteDoesNotClose(t *testing.T) {
	wire := &prefixFailureConn{calls: 1}
	c := &Conn{conn: wire, state: StatePlay, playOK: true}
	handler := syntheticHandler(c)
	handler(syntheticFrame(1))
	handler(syntheticFrame(2))
	if wire.closed || wire.calls != 3 || wire.wire.Len() != 72 {
		t.Fatalf("complete writes changed: closed=%v calls=%d bytes=%d", wire.closed, wire.calls, wire.wire.Len())
	}
}
func TestInterleavedErrorBeforeBytesAlsoCloses(t *testing.T) {
	wire := &prefixFailureConn{prefix: 0, failure: errors.New("synthetic timeout")}
	c := &Conn{conn: wire, state: StatePlay, playOK: true}
	handler := syntheticHandler(c)
	handler(syntheticFrame(1))
	handler(syntheticFrame(2))
	if !wire.closed || wire.calls != 1 {
		t.Fatalf("continued failed connection: closed=%v calls=%d", wire.closed, wire.calls)
	}
}

type boundedDeadlineConn struct{ net.Conn }

func (c boundedDeadlineConn) SetWriteDeadline(time.Time) error {
	return c.Conn.SetWriteDeadline(time.Now().Add(20 * time.Millisecond))
}
func TestInterleavedNetPipePrefixTimeoutDoesNotAppendNewFrame(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	c := &Conn{conn: boundedDeadlineConn{server}, state: StatePlay, playOK: true}
	handler := syntheticHandler(c)
	firstDone := make(chan struct{})
	go func() { handler(syntheticFrame(1)); close(firstDone) }()
	prefix := make([]byte, 7)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, prefix); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("write did not finish after prefix timeout")
	}
	secondDone := make(chan struct{})
	go func() { handler(syntheticFrame(2)); close(secondDone) }()
	next := make([]byte, 36)
	n, err := io.ReadFull(client, next)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("next handler blocked")
	}
	if n != 0 || err != io.EOF {
		t.Fatalf("new frame appended after incomplete prefix: bytes=%d err=%v prefixRTPSize=%d nextStart=%x", n, err, uint16(prefix[2])<<8|uint16(prefix[3]), next[:4])
	}
}

func TestInterleavedFailureSerializesPendingControlResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	c := &Conn{conn: boundedDeadlineConn{server}, state: StatePlay, playOK: true}
	handler := syntheticHandler(c)
	firstDone := make(chan struct{})
	go func() { handler(syntheticFrame(1)); close(firstDone) }()
	prefix := make([]byte, 7)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(client, prefix); err != nil {
		t.Fatal(err)
	}
	responseDone := make(chan error, 1)
	go func() { responseDone <- c.WriteResponse(&tcp.Response{Status: "200 OK"}) }()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("sender did not exit")
	}
	select {
	case err := <-responseDone:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("queued response result=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("response deadlocked")
	}
	b := make([]byte, 64)
	n, err := client.Read(b)
	if n != 0 || err != io.EOF {
		t.Fatalf("control response appended after partial RTP: n=%d err=%v", n, err)
	}
}

func TestDialResetsFailedTransportForNewConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	c := NewClient(fmt.Sprintf("rtsp://%s/example", listener.Addr()))
	c.conn = &prefixFailureConn{prefix: 7, failure: io.ErrUnexpectedEOF}
	c.state = StatePlay
	c.playOK = true
	syntheticHandler(c)(syntheticFrame(1))
	if err = c.Dial(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	handler := syntheticHandler(c)
	c.state = StatePlay
	c.playOK = true
	done := make(chan struct{})
	go func() { handler(syntheticFrame(1)); close(done) }()
	data := make([]byte, 36)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = io.ReadFull(peer, data); err != nil {
		t.Fatalf("new transport retained old failure: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("new handler blocked")
	}
}
