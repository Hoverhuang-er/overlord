package redis

import (
	"bytes"
	errs "errors"
	"fmt"
	"github.com/Hoverhuang-er/overlord/pkg/bufio"
	"github.com/Hoverhuang-er/overlord/pkg/log"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/pkg/stackerr"
	"github.com/Hoverhuang-er/overlord/proxy/proto"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

const (
	opened = int32(0)
	closed = int32(1)

	nodeReadBufSize = 2 * 1024 * 1024 // NOTE: 2MB
)

var (
	// ErrNodeConnClosed err node conn closed.
	ErrNodeConnClosed    = errs.New("redis node conn closed")
	cmdAuthWithPassBytes = func(password string) []byte {
		return bytes.NewBufferString(fmt.Sprintf("AUTH %s\r\n", password)).Bytes()
	}
)

// NodeConn is export type by nodeConn for redis-cluster.
type NodeConn = nodeConn

// Bw return bufio.Writer.
func (nc *NodeConn) Bw() *bufio.Writer {
	return nc.bw
}

type nodeConn struct {
	cluster string
	addr    string
	conn    *libnet.Conn
	bw      *bufio.Writer
	br      *bufio.Reader
	passwd  string
	state   int32
	authed  bool
}

func (nc *nodeConn) Password() string {
	//TODO implement me
	return nc.passwd
}

// NewNodeConn create the node conn from proxy to redis
func NewNodeConn(cluster, addr, password string, dialTimeout, readTimeout, writeTimeout time.Duration) (nc *nodeConn) {
	conn := libnet.DialWithTimeout(addr, dialTimeout, readTimeout, writeTimeout)
	var nnc = newNodeConn(cluster, addr, password, conn)
	nnc.DoAuth()
	return nnc
}

func newNodeConn(cluster, addr, password string, conn *libnet.Conn) *nodeConn {
	return &nodeConn{
		cluster: cluster,
		addr:    addr,
		conn:    conn,
		br:      bufio.NewReader(conn, bufio.Get(nodeReadBufSize)),
		bw:      bufio.NewWriter(conn),
		passwd:  password,
	}
}

func (f *nodeConn) DoAuth() error {
	if err := f.bw.Write(cmdAuthWithPassBytes(f.Password())); err != nil {
		log.Errorf("Failed to auth with password :%v", err)
	}
	if err := f.bw.Flush(); err != nil {
		log.Errorf("Failed to auth with password :%v", err)
		return stackerr.ReplaceErrStack(err)
	}
	log.Info("Write Auth CMD to Redis")
	var authdata []byte
	begin1 := f.br.Mark()
	for {
		err := f.br.Read()
		if err != nil {
			return stackerr.ReplaceErrStack(err)
		}
		reply := &RESP{}
		if err = reply.Decode(f.br); errs.Is(err, bufio.ErrBufferFull) {
			f.br.AdvanceTo(begin1)
			continue
		} else if err != nil {
			return stackerr.ReplaceErrStack(err)
		}
		if reply.Type() != respString {
			return stackerr.ReplaceErrStack(err)
		}
		authdata = reply.Data()
		idx := bytes.Index(authdata, crlfBytes)
		authdata = authdata[idx+2:]
		break
	}
	return nil
}

func (nc *nodeConn) Addr() string {
	return nc.addr
}

func (nc *nodeConn) Cluster() string {
	return nc.cluster
}

func (nc *nodeConn) Write(m *proto.Message) (err error) {
	if nc.Closed() {
		err = errors.WithStack(ErrNodeConnClosed)
		return
	}
	req, ok := m.Request().(*Request)
	if !ok {
		err = errors.WithStack(ErrBadAssert)
		return
	}
	if !req.IsSupport() || req.IsCtl() {
		return
	}
	if err = req.resp.encode(nc.bw); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	return
}

func (nc *nodeConn) Flush() error {
	if nc.Closed() {
		return errors.WithStack(ErrNodeConnClosed)
	}
	return nc.bw.Flush()
}

func (nc *nodeConn) Read(m *proto.Message) (err error) {
	if nc.Closed() {
		err = errors.WithStack(ErrNodeConnClosed)
		return
	}
	req, ok := m.Request().(*Request)
	if !ok {
		err = errors.WithStack(ErrBadAssert)
		return
	}
	if !req.IsSupport() || req.IsCtl() {
		return
	}
	for {
		if err = req.reply.decode(nc.br); err == bufio.ErrBufferFull {
			if err = nc.br.Read(); err != nil {
				return stackerr.ReplaceErrStack(err)
				return
			}
			continue
		} else if err != nil {
			return stackerr.ReplaceErrStack(err)
			return
		}
		return
	}
}

func (nc *nodeConn) Close() (err error) {
	if atomic.CompareAndSwapInt32(&nc.state, opened, closed) {
		return nc.conn.Close()
	}
	return
}

func (nc *nodeConn) Closed() bool {
	return atomic.LoadInt32(&nc.state) == closed
}
