package binary

import (
	"bytes"
	"encoding/binary"
	"github.com/Hoverhuang-er/overlord/pkg/stackerr"
	"sync/atomic"
	"time"

	"github.com/Hoverhuang-er/overlord/pkg/bufio"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/proxy/proto"

	"github.com/pkg/errors"
)

const (
	opened = int32(0)
	closed = int32(1)

	nodeReadBufSize = 2 * 1024 * 1024 // NOTE: 2MB
)

type nodeConn struct {
	cluster string
	addr    string

	conn     *libnet.Conn
	bw       *bufio.Writer
	br       *bufio.Reader
	password string
	state    int32
}

func (n *nodeConn) Password() string {
	//TODO implement me
	return n.password
}

// NewNodeConn returns node conn.
func NewNodeConn(cluster, addr string, dialTimeout, readTimeout, writeTimeout time.Duration) (nc proto.NodeConn) {
	conn := libnet.DialWithTimeout(addr, dialTimeout, readTimeout, writeTimeout)
	nc = &nodeConn{
		cluster: cluster,
		addr:    addr,
		conn:    conn,
		bw:      bufio.NewWriter(conn),
		br:      bufio.NewReader(conn, bufio.Get(nodeReadBufSize)),
	}
	return
}

func (n *nodeConn) Addr() string {
	return n.addr
}

func (n *nodeConn) Cluster() string {
	return n.cluster
}

func (n *nodeConn) Write(m *proto.Message) (err error) {
	if n.Closed() {
		err = errors.WithStack(ErrClosed)
		return
	}
	mcr, ok := m.Request().(*MCRequest)
	if !ok {
		err = errors.WithStack(ErrAssertReq)
		return
	}
	_ = n.bw.Write(magicReqBytes)

	cmd := mcr.respType
	if cmd == RequestTypeGetQ || cmd == RequestTypeGetKQ {
		cmd = RequestTypeGetK
	}
	_ = n.bw.Write(cmd.Bytes())
	_ = n.bw.Write(mcr.keyLen)
	_ = n.bw.Write(mcr.extraLen)
	_ = n.bw.Write(zeroBytes)
	_ = n.bw.Write(zeroTwoBytes)
	_ = n.bw.Write(mcr.bodyLen)
	_ = n.bw.Write(mcr.opaque)
	err = n.bw.Write(mcr.cas)
	if !bytes.Equal(mcr.bodyLen, zeroFourBytes) {
		err = n.bw.Write(mcr.data)
	}
	return
}

func (n *nodeConn) Flush() error {
	if n.Closed() {
		return errors.WithStack(ErrClosed)
	}
	return n.bw.Flush()
}

func (n *nodeConn) Read(m *proto.Message) (err error) {
	if n.Closed() {
		err = errors.WithStack(ErrClosed)
		return
	}
	mcr, ok := m.Request().(*MCRequest)
	if !ok {
		err = errors.WithStack(ErrAssertReq)
		return
	}
	mcr.data = mcr.data[:0]
REREAD:
	var bs []byte
	if bs, err = n.br.ReadExact(requestHeaderLen); err == bufio.ErrBufferFull {
		if err = n.br.Read(); err != nil {
			return stackerr.ReplaceErrStack(err)
			return
		}
		goto REREAD
	} else if err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	parseHeader(bs, mcr, false)
	bl := binary.BigEndian.Uint32(mcr.bodyLen)
	if bl == 0 {
		return
	}
REREADData:
	var data []byte
	if data, err = n.br.ReadExact(int(bl)); err == bufio.ErrBufferFull {
		if err = n.br.Read(); err != nil {
			return stackerr.ReplaceErrStack(err)
			return
		}
		goto REREADData
	} else if err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	mcr.data = append(mcr.data, data...)
	return
}

func (n *nodeConn) Close() error {
	if atomic.CompareAndSwapInt32(&n.state, opened, closed) {
		return n.conn.Close()
	}
	return nil
}

func (n *nodeConn) Closed() bool {
	return atomic.LoadInt32(&n.state) == closed
}
