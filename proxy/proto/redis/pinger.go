package redis

import (
	"bytes"
	errs "errors"
	"github.com/Hoverhuang-er/overlord/pkg/stackerr"
	"sync/atomic"

	"github.com/Hoverhuang-er/overlord/pkg/bufio"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/proxy/proto"

	"github.com/pkg/errors"
)

const (
	pingBufferSize = 128
)

// stackerr
var (
	ErrPingClosed = errs.New("ping interface has been closed")
	ErrBadPong    = errs.New("pong response payload is bad")
)

var (
	pingBytes = []byte("*1\r\n$4\r\nPING\r\n")
	pongBytes = []byte("+PONG\r\n")
)

type pinger struct {
	conn *libnet.Conn

	br *bufio.Reader
	bw *bufio.Writer

	state int32
}

// NewPinger new pinger.
func NewPinger(conn *libnet.Conn) proto.Pinger {
	return &pinger{
		conn:  conn,
		br:    bufio.NewReader(conn, bufio.NewBuffer(pingBufferSize)),
		bw:    bufio.NewWriter(conn),
		state: opened,
	}
}

func (p *pinger) Ping() (err error) {
	if atomic.LoadInt32(&p.state) == closed {
		err = errors.WithStack(ErrPingClosed)
		return
	}
	_ = p.bw.Write(pingBytes)
	if err = p.bw.Flush(); err != nil {
		err = stackerr.ReplaceErrStack(err)
		return
	}
	_ = p.br.Read()
	defer p.br.Buffer().Reset()
	data, err := p.br.ReadLine()
	if err != nil {
		err = stackerr.ReplaceErrStack(err)
		return
	}
	if !bytes.Equal(data, pongBytes) {
		err = errors.WithStack(ErrBadPong)
	}
	return
}

func (p *pinger) Close() error {
	if atomic.CompareAndSwapInt32(&p.state, opened, closed) {
		return p.conn.Close()
	}
	return nil
}
