package cluster

import (
	"bytes"
	errs "errors"
	"strconv"

	"github.com/Hoverhuang-er/overlord/pkg/bufio"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/proxy/proto/redis"

	"github.com/pkg/errors"
)

const (
	respFetch = '$'
)

var (
	crlfBytes = []byte("\r\n")
)

// fetcher will execute `CLUSTER NODES` by the given address。
type fetcher struct {
	conn *libnet.Conn
	bw   *bufio.Writer
	br   *bufio.Reader
	auth FetchAuth
}

type FetchAuth struct {
	authorized bool
	useTls     bool
	password   string
	cafile     string
	certfile   string
	keyfile    string
}

var (
	cmdClusterNodesBytes = []byte("*2\r\n$7\r\nCLUSTER\r\n$5\r\nNODES\r\n")
	cmdAuthBytes         = func(password string) []byte {
		return strconv.AppendQuote([]byte("*2\r\n$4\r\nAUTH\r\n$"), password)
	}
	// ErrBadReplyType error bad reply type
	ErrBadReplyType = errs.New("fetcher CLUSTER NODES bad reply type")
)

// newFetcher will create new fetcher
func newFetcher(conn *libnet.Conn) *fetcher {
	f := &fetcher{
		conn: conn,
		br:   bufio.NewReader(conn, bufio.Get(4096)),
		bw:   bufio.NewWriter(conn),
	}
	return f
}

// newFetcher will create new fetcher
func newFetcherWhAuth(conn *libnet.Conn, cc *cluster) *fetcher {
	f := &fetcher{
		conn: conn,
		br:   bufio.NewReader(conn, bufio.Get(4096)),
		bw:   bufio.NewWriter(conn),
		auth: FetchAuth{
			authorized: true,
			useTls:     false,
			password:   cc.password,
		},
	}
	return f
}

// Fetch new CLUSTER NODES result
func (f *fetcher) fetch() (ns *nodeSlots, err error) {
	if err = f.bw.Write(cmdClusterNodesBytes); err != nil {
		err = errors.WithStack(err)
		return
	}
	if err = f.bw.Flush(); err != nil {
		err = errors.WithStack(err)
		return
	}
	var data []byte
	begin := f.br.Mark()
	for {
		err = f.br.Read()
		if err != nil {
			err = errors.WithStack(err)
			return
		}
		reply := &redis.RESP{}
		if err = reply.Decode(f.br); err == bufio.ErrBufferFull {
			f.br.AdvanceTo(begin)
			continue
		} else if err != nil {
			err = errors.WithStack(err)
			return
		}
		if reply.Type() != respFetch {
			err = errors.WithStack(ErrBadReplyType)
			return
		}
		data = reply.Data()
		idx := bytes.Index(data, crlfBytes)
		data = data[idx+2:]
		break
	}
	return parseSlots(data)
}

// Fetch new CLUSTER NODES result
func (f *fetcher) fetchAuth() (ns *nodeSlots, err error) {
	if err = f.bw.Write(cmdAuthBytes(f.auth.password)); err != nil {
		err = errors.WithStack(err)
		return
	}
	if err = f.bw.Write(cmdClusterNodesBytes); err != nil {
		err = errors.WithStack(err)
		return
	}
	if err = f.bw.Flush(); err != nil {
		err = errors.WithStack(err)
		return
	}
	var data []byte
	begin := f.br.Mark()
	for {
		err = f.br.Read()
		if err != nil {
			err = errors.WithStack(err)
			return
		}
		reply := &redis.RESP{}
		if err = reply.Decode(f.br); err == bufio.ErrBufferFull {
			f.br.AdvanceTo(begin)
			continue
		} else if err != nil {
			err = errors.WithStack(err)
			return
		}
		if reply.Type() != respFetch {
			err = errors.WithStack(ErrBadReplyType)
			return
		}
		data = reply.Data()
		idx := bytes.Index(data, crlfBytes)
		data = data[idx+2:]
		break
	}
	return parseSlots(data)
}

// Close enable to close the conneciton of backend.
func (f *fetcher) Close() error {
	return f.conn.Close()
}
