package proxy

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hoverhuang-er/overlord/pkg/log"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/pkg/prom"
	"github.com/Hoverhuang-er/overlord/pkg/types"
	"github.com/Hoverhuang-er/overlord/proxy/proto"
	"github.com/Hoverhuang-er/overlord/proxy/proto/memcache"
	mcbin "github.com/Hoverhuang-er/overlord/proxy/proto/memcache/binary"
	"github.com/Hoverhuang-er/overlord/proxy/proto/redis"
	rclstr "github.com/Hoverhuang-er/overlord/proxy/proto/redis/cluster"
	"github.com/Hoverhuang-er/overlord/proxy/slowlog"

	"github.com/pkg/errors"
)

const (
	handlerOpening = int32(0)
	handlerClosed  = int32(1)
)

// variables need to change
var (
	// TODO: config and reduce to small
	concurrent    = 2
	maxConcurrent = 1024
)

// Handler handle conn.
type Handler struct {
	p  *Proxy
	cc *ClusterConfig

	slog       slowlog.Handler
	slowerThan time.Duration

	forwarder proto.Forwarder

	conn *libnet.Conn
	pc   proto.ProxyConn

	closed int32
	err    error
}

// NewHandler new a conn handler.
func NewHandler(p *Proxy, cc *ClusterConfig, conn net.Conn, forwarder proto.Forwarder) (h *Handler) {
	h = &Handler{
		p:         p,
		cc:        cc,
		forwarder: forwarder,
	}

	if cc.SlowlogSlowerThan != 0 {
		h.slowerThan = time.Duration(cc.SlowlogSlowerThan) * time.Microsecond
		h.slog = slowlog.Get(cc.Name)
	}

	h.conn = libnet.NewConn(conn, time.Second*time.Duration(h.p.c.Proxy.ReadTimeout), time.Second*time.Duration(h.p.c.Proxy.WriteTimeout))
	// cache type
	switch cc.CacheType {
	case types.CacheTypeMemcache:
		h.pc = memcache.NewProxyConn(h.conn)
	case types.CacheTypeMemcacheBinary:
		h.pc = mcbin.NewProxyConn(h.conn)
	case types.CacheTypeRedis:
		//h.pc = redis.NewProxyConn(h.conn, h.cc.Password)
		h.pc = redis.NewProxyConnV2(h.conn, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)
	case types.CacheTypeRedisCluster:
		//h.pc = rclstr.NewProxyConn(h.conn, forwarder, h.cc.Password, cc)
		h.pc = rclstr.NewProxyConnV2(h.conn, forwarder, cc.ToRedis.Auth.CaFile, cc.ToRedis.Auth.CertFile, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)

	default:
		panic(types.ErrNoSupportCacheType)
	}
	prom.ConnIncr(cc.Name)
	return
}

// Handle reads Msg from client connection and dispatchs Msg back to cache servers,
// then reads response from cache server and writes response into client connection.
func (h *Handler) Handle() {
	go h.handle()
}

func (h *Handler) handle() {
	var (
		messages []*proto.Message
		msgs     []*proto.Message
		wg       = &sync.WaitGroup{}
		err      error
	)
	messages = h.allocMaxConcurrent(wg, messages, len(msgs))
	for {
		// 1. read until limit or error
		if msgs, err = h.pc.Decode(messages); err != nil {
			h.deferHandle(messages, err)
			return
		}

		// 2. handle special command
		isSpecialCmd := false
		if len(msgs) > 0 {
			isSpecialCmd, err = h.pc.CmdCheck(msgs[0])
			if err != nil {
				h.pc.Flush()
				h.deferHandle(messages, err)
				return
			}
		}

		if !isSpecialCmd && h.pc.IsAuthorized() {
			// 3. send to cluster
			h.forwarder.Forward(msgs)
			wg.Wait()

			// 4. encode
			for _, msg := range msgs {
				msg.MarkEndPipe()
				if err = h.pc.Encode(msg); err != nil {
					h.pc.Flush()
					h.deferHandle(messages, err)
					return
				}
				msg.MarkEnd()
				if prom.On {
					prom.ProxyTime(h.cc.Name, msg.Request().CmdString(), int64(msg.TotalDur()/time.Microsecond))
				}
			}
		}

		if err = h.pc.Flush(); err != nil {
			h.deferHandle(messages, err)
			return
		}

		// 5. check slowlog before release resource
		if h.slowerThan != 0 {
			for _, msg := range msgs {
				if msg.TotalDur() > h.slowerThan {
					h.slog.Record(msg.Slowlog())
				}
			}
		}

		for _, msg := range msgs {
			msg.ResetSubs()
			msg.Reset()
		}
		// 6. alloc MaxConcurrent
		messages = h.allocMaxConcurrent(wg, messages, len(msgs))
	}
}

func (h *Handler) allocMaxConcurrent(wg *sync.WaitGroup, msgs []*proto.Message, lastCount int) []*proto.Message {
	var alloc int
	if msgsLength := len(msgs); msgsLength == 0 {
		alloc = concurrent
	} else if msgsLength < maxConcurrent && msgsLength == lastCount {
		alloc = msgsLength * concurrent
	}
	if alloc > 0 {
		proto.PutMsgs(msgs)
		msgs = proto.GetMsgs(alloc) // TODO: change the msgs by lastCount trending
		for _, msg := range msgs {
			msg.WithWaitGroup(wg)
		}
	}
	return msgs
}

func (h *Handler) deferHandle(msgs []*proto.Message, err error) {
	proto.PutMsgs(msgs)
	h.closeWithError(err)
	return
}

func (h *Handler) closeWithError(err error) {
	if atomic.CompareAndSwapInt32(&h.closed, handlerOpening, handlerClosed) {
		h.err = err
		_ = h.conn.Close()
		atomic.AddInt32(&h.p.conns, -1) // NOTE: decr!!!
		if err == proto.ErrQuit {
			return
		}
		if prom.On {
			prom.ConnDecr(h.cc.Name)
		}
		if errors.Cause(err) != io.EOF {
			log.Warnf("cluster(%s) addr(%s) remoteAddr(%s) handler close error:%+v", h.cc.Name, h.cc.ListenAddr, h.conn.RemoteAddr(), err)
		}
	}
}
