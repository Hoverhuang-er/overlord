package cluster

import (
	"bytes"
	errs "errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hoverhuang-er/overlord/pkg/hashkit"
	"github.com/Hoverhuang-er/overlord/pkg/log"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/proxy/proto"
)

const (
	opening = int32(0)
	closed  = int32(1)

	musk = 0x3fff
)

// stackerr
var (
	ErrClusterClosed = errs.New("cluster executor already closed")
)

const (
	fakeClusterNodes = "" +
		"0000000000000000000000000000000000000001 {ADDR} master - 0 0 1 connected 0-5460\n" +
		"0000000000000000000000000000000000000002 {ADDR} master - 0 0 2 connected 5461-10922\n" +
		"0000000000000000000000000000000000000003 {ADDR} master - 0 0 3 connected 10923-16383\n"

	fakeClusterSlots = "" +
		"*3\r\n" +
		"*3\r\n:0\r\n:5460\r\n*2\r\n${IPLEN}\r\n{IP}\r\n:{PORT}\r\n" +
		"*3\r\n:5461\r\n:10922\r\n*2\r\n${IPLEN}\r\n{IP}\r\n:{PORT}\r\n" +
		"*3\r\n:10923\r\n:16383\r\n*2\r\n${IPLEN}\r\n{IP}\r\n:{PORT}\r\n"
)

type cluster struct {
	name          string
	password      string
	servers       []string
	conns         int32
	dto, rto, wto time.Duration
	hashTag       []byte

	slotNode atomic.Value
	action   chan struct{}

	fakeNodesBytes []byte
	fakeSlotsBytes []byte
	once           sync.Once

	state int32
}

// NewForwarder new proto Forwarder.
func NewForwarder(name, listen string, servers []string, conns int32, dto, rto, wto time.Duration, hashTag []byte) proto.Forwarder {
	c := &cluster{
		name:    name,
		servers: servers,
		conns:   conns,
		dto:     dto,
		rto:     rto,
		wto:     wto,
		hashTag: hashTag,
		action:  make(chan struct{}),
	}
	if !c.tryFetch() {
		_ = c.Close()
		log.Warnf("fail to init fetch cluster all seeds nodes cluster down but continue")
		return c
	}
	c.fake(listen)
	go c.fetchproc()
	return c
}

func NewForwarderWithAuth(name, listen, password string, servers []string, conns int32, dto, rto, wto time.Duration, hashTag []byte) proto.Forwarder {
	c := &cluster{
		name:     name,
		servers:  servers,
		conns:    conns,
		dto:      dto,
		rto:      rto,
		wto:      wto,
		hashTag:  hashTag,
		password: password,
		action:   make(chan struct{}),
	}
	if !c.tryFetch() {
		_ = c.Close()
		log.Warn("fail to init fetch cluster all seeds nodes cluster down but continue")
		return c
	}
	c.fake(listen)
	go c.fetchproc()
	return c
}

func (c *cluster) Forward(msgs []*proto.Message) error {
	if state := atomic.LoadInt32(&c.state); state == closed {
		return ErrClusterClosed
	}
	for _, m := range msgs {
		if m.IsBatch() {
			for _, subm := range m.Batch() {
				ncp := c.getPipe(subm.Request().Key())
				subm.MarkStartPipe()
				ncp.Push(subm)
			}
		} else {
			ncp := c.getPipe(m.Request().Key())
			m.MarkStartPipe()
			ncp.Push(m)
		}
	}
	return nil
}

// Don't support update backend server list now
func (c *cluster) Update([]string) error {
	return nil
}

func (c *cluster) Close() error {
	if !atomic.CompareAndSwapInt32(&c.state, opening, closed) {
		sn := c.slotNode.Load()
		if sn == nil {
			return nil
		}
		np := sn.(*slotNode)
		for _, npc := range np.nodePipe {
			npc.Close()
		}
		return nil
	}
	return nil
}

func (c *cluster) getPipe(key []byte) (ncp *proto.NodeConnPipe) {
	realKey := c.trimHashTag(key)
	crc := hashkit.Crc16(realKey) & musk
	sn := c.slotNode.Load().(*slotNode)
	addr := sn.nSlots.slots[crc]
	ncp = sn.nodePipe[addr]
	return
}

func (c *cluster) trimHashTag(key []byte) []byte {
	if len(c.hashTag) != 2 {
		return key
	}
	bidx := bytes.IndexByte(key, c.hashTag[0])
	if bidx == -1 {
		return key
	}
	eidx := bytes.IndexByte(key[bidx+1:], c.hashTag[1])
	if eidx == -1 {
		return key
	}
	return key[bidx+1 : bidx+1+eidx]
}

func (c *cluster) fetchproc() {
	for {
		select {
		case <-c.action:
		case <-time.After(30 * time.Minute):
		}
		c.tryFetch()
		time.Sleep(time.Second)
	}
}

func (c *cluster) tryFetch() bool {
	// for map's access is random in golang.
	shuffleMap := make(map[string]struct{})
	for _, server := range c.servers {
		shuffleMap[server] = struct{}{}
	}
	var connectedNode int
	for server := range shuffleMap {
		switch c.password {
		case "":
			log.Infof("Connect redis with no auth")
			conn := libnet.DialWithTimeout(server, c.dto, c.rto, c.wto)
			f := newFetcher(conn)
			nSlots, err := f.fetch()
			if err != nil {
				log.Errorf("Redis Cluster fail to fetch error:%v", err)

				continue
			}
			connectedNode++
			c.initSlotNode(nSlots)
			log.Warn("Redis Cluster try fetch success")

		default:
			log.Infof("Connect redis with auth")
			conn := libnet.DialWithTimeoutWithAuth(server, c.password, c.dto, c.rto, c.wto)
			f := newFetcherWhAuth(conn, c)
			nSlots, err := f.fetchAuth()
			if err != nil {
				log.Errorf("Redis Cluster fail to fetch error:%v", err)

				continue
			}
			connectedNode++
			c.initSlotNode(nSlots)
			log.Warn("Redis Cluster try fetch success")

		}
		log.Infof("Redis Cluster NodeConnected (%d/%d) already connect", connectedNode, len(shuffleMap))
		return true
	}
	log.Error("Redis Cluster all seed nodes fail to fetch")

	return false
}

func (c *cluster) initSlotNode(nSlots *nodeSlots) {
	osn, ok := c.slotNode.Load().(*slotNode) // old slotNode
	oncp := map[string]*proto.NodeConnPipe{} // old nodeConn
	if ok && osn != nil {
		for addr, ncp := range osn.nodePipe {
			oncp[addr] = ncp // COPY
		}
	}
	sn := &slotNode{nSlots: nSlots}
	sn.nodePipe = make(map[string]*proto.NodeConnPipe)
	masters := nSlots.getMasters()
	for _, addr := range masters {
		ncp, ok := oncp[addr]
		if !ok {
			toAddr := addr // NOTE: avoid closure
			ncp = proto.NewNodeConnPipe(c.conns, func() proto.NodeConn {
				return newNodeConn(c, toAddr)
			})
			go c.pipeEvent(ncp.ErrorEvent())
			log.Warnf("Redis Cluster renew slot node and add addr:%s", toAddr)
		} else {
			delete(oncp, addr)
		}
		sn.nodePipe[addr] = ncp
	}
	c.servers = masters
	c.slotNode.Store(sn)
	for addr, ncp := range oncp {
		ncp.Close()
		log.Warnf("Redis Cluster renew slot node and close addr:%s", addr)
	}
}

func (c *cluster) pipeEvent(errCh <-chan error) {
	for {
		err, ok := <-errCh
		if !ok {
			return
		}
		log.Errorf("Redis Cluster NodeConnPipe action error:%v", err)

		c.action <- struct{}{}
	}
}

func (c *cluster) fake(listen string) {
	c.once.Do(func() {
		_, port, err := net.SplitHostPort(listen)
		if err != nil {
			panic(err)
		}
		inters, err := net.Interfaces()
		if err != nil {
			panic(err)
		}
		for _, inter := range inters {
			if strings.HasPrefix(inter.Name, "lo") {
				continue
			}
			addrs, err := inter.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
					ipStr := ipnet.IP.String()

					ipStrLen := len(ipStr)
					slotsStr := strings.Replace(fakeClusterSlots, "{IPLEN}", strconv.Itoa(ipStrLen), -1)
					slotsStr = strings.Replace(slotsStr, "{IP}", ipStr, -1)
					slotsStr = strings.Replace(slotsStr, "{PORT}", port, -1)
					c.fakeSlotsBytes = []byte(slotsStr)

					nodesStr := strings.Replace(fakeClusterNodes, "{ADDR}", net.JoinHostPort(ipStr, port), -1)
					nodesLen := len(nodesStr)
					c.fakeNodesBytes = []byte("$" + strconv.Itoa(nodesLen) + "\r\n" + nodesStr + "\r\n")
					return
				}
			}
		}
	})
}

type slotNode struct {
	nSlots   *nodeSlots
	nodePipe map[string]*proto.NodeConnPipe
}
