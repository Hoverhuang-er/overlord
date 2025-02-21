package cluster

import (
	"bytes"
	"github.com/Hoverhuang-er/overlord/pkg/stackerr"
	"strings"
	"sync/atomic"

	"github.com/Hoverhuang-er/overlord/pkg/conv"
	"github.com/Hoverhuang-er/overlord/pkg/log"
	"github.com/Hoverhuang-er/overlord/proxy/proto"
	"github.com/Hoverhuang-er/overlord/proxy/proto/redis"
)

const (
	respRedirect = '-'
	maxRedirects = 5
)

var (
	askBytes   = []byte("ASK")
	movedBytes = []byte("MOVED")

	askingResp = []byte("*1\r\n$6\r\nASKING\r\n")
)

type nodeConn struct {
	c    *cluster
	addr string
	nc   proto.NodeConn

	sb strings.Builder

	redirects int
	password  string
	state     int32
}

func (nc *nodeConn) Password() string {
	//TODO implement me
	return nc.password
}

func newNodeConn(c *cluster, addr string) (nc proto.NodeConn) {
	nc = &nodeConn{
		c:    c,
		addr: addr,
		nc:   redis.NewNodeConn(c.name, addr, c.password, c.dto, c.rto, c.wto),
	}
	return
}

func (nc *nodeConn) Addr() string {
	return nc.addr
}

func (nc *nodeConn) Cluster() string {
	return nc.c.name
}

func (nc *nodeConn) Write(m *proto.Message) (err error) {
	if err := nc.nc.Write(m); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	return
}

func (nc *nodeConn) Flush() error {
	return nc.nc.Flush()
}

func (nc *nodeConn) Read(m *proto.Message) (err error) {
	if err = nc.nc.Read(m); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	req := m.Request().(*redis.Request)
	// check request
	if !req.IsSupport() || req.IsCtl() {
		return
	}
	reply := req.Reply()
	if reply.Type() != respRedirect {
		return
	}
	if nc.redirects >= maxRedirects { // NOTE: check max redirects
		log.Warnf("Redis Cluster NodeConn key(%s) already max redirects", req.Key())

		return
	}
	data := reply.Data()
	if !bytes.HasPrefix(data, askBytes) && !bytes.HasPrefix(data, movedBytes) {
		return
	}

	addrBs, _, isAsk, _ := parseRedirect(data)
	if !isAsk {
		// tryFetch when key moved
		select {
		case nc.c.action <- struct{}{}:
		default:
		}
	}
	nc.sb.Reset()
	nc.sb.Write(addrBs)
	addr := nc.sb.String()
	// redirect process
	if err = nc.redirectProcess(m, req, addr, isAsk); err != nil {
		log.Errorf("Redis Cluster NodeConn redirectProcess addr:%s error:%v", addr, err)
	}
	nc.redirects = 0
	return
}

func (nc *nodeConn) redirectProcess(m *proto.Message, req *redis.Request, addr string, isAsk bool) (err error) {
	// next redirect
	nc.redirects++
	log.Warnf("Redis Cluster NodeConn key(%s) redirect count(%d)", req.Key(), nc.redirects)

	// start redirect
	nnc := newNodeConn(nc.c, addr)
	tmp := nnc.(*nodeConn)
	tmp.redirects = nc.redirects // NOTE: for check max redirects
	rnc := tmp.nc.(*redis.NodeConn)
	defer nnc.Close()
	if isAsk {
		if err = rnc.Bw().Write(askingResp); err != nil {
			return stackerr.ReplaceErrStack(err)
		}
	}
	if err = req.RESP().Encode(rnc.Bw()); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	if err = rnc.Bw().Flush(); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	// NOTE: even if the client waits a long time before reissuing the query, and in the meantime the cluster configuration
	// changed, the destination node will reply again with a MOVED error if the hash slot is now served by another node.
	if err = nnc.Read(m); err != nil {
		return stackerr.ReplaceErrStack(err)
	}
	return
}

func (nc *nodeConn) Close() (err error) {
	if atomic.CompareAndSwapInt32(&nc.state, opening, closed) {
		return nc.nc.Close()
	}
	return
}

func parseRedirect(data []byte) (addr []byte, slot int, isAsk bool, err error) {
	fields := bytes.Fields(data)
	if len(fields) != 3 {
		return
	}
	si, err := conv.Btoi(fields[1])
	if err != nil {
		return
	}
	addr = fields[2]
	slot = int(si)
	isAsk = bytes.Equal(askBytes, fields[0])
	return
}
