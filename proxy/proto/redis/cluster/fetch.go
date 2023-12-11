package cluster

import (
	"bytes"
	errs "errors"
	"fmt"
	"github.com/Hoverhuang-er/overlord/pkg/bufio"
	"github.com/Hoverhuang-er/overlord/pkg/log"
	libnet "github.com/Hoverhuang-er/overlord/pkg/net"
	"github.com/Hoverhuang-er/overlord/proxy/proto/redis"

	"github.com/pkg/errors"
)

const (
	respFetch  = '$'
	respString = '+'
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
		return bytes.NewBufferString(fmt.Sprintf("AUTH %s\r\n", password)).Bytes()
	}
	// ErrBadReplyType error bad reply type
	ErrBadReplyType = errs.New("fetcher CLUSTER NODES bad reply type")
	ErrNotEqualOk   = errs.New("fetcher REDIS Node Auth not equal ok")
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
		err = stackerr.ReplaceErrStack(err)
		return
	}
	if err = f.bw.Flush(); err != nil {
		err = stackerr.ReplaceErrStack(err)
		return
	}
	var data []byte
	begin := f.br.Mark()
	for {
		err = f.br.Read()
		if err != nil {
			err = stackerr.ReplaceErrStack(err)
			return
		}
		reply := &redis.RESP{}
		if err = reply.Decode(f.br); err == bufio.ErrBufferFull {
			f.br.AdvanceTo(begin)
			continue
		} else if err != nil {
			err = stackerr.ReplaceErrStack(err)
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
		log.Errorf("Failed to auth with password :%v", err)
		err = stackerr.ReplaceErrStack(err)
		return
	}
	if err = f.bw.Flush(); err != nil {
		log.Errorf("Failed to auth with password :%v", err)
		err = stackerr.ReplaceErrStack(err)
		return
	}
	log.Info("Write Auth CMD to Redis")
	var authdata []byte
	begin1 := f.br.Mark()
	for {
		err = f.br.Read()
		if err != nil {
			err = stackerr.ReplaceErrStack(err)
			return
		}
		reply := &redis.RESP{}
		if err = reply.Decode(f.br); err == bufio.ErrBufferFull {
			f.br.AdvanceTo(begin1)
			continue
		} else if err != nil {
			err = stackerr.ReplaceErrStack(err)
			return
		}
		if reply.Type() != respString {
			err = errors.WithStack(ErrNotEqualOk)
			return
		}
		authdata = reply.Data()
		idx := bytes.Index(authdata, crlfBytes)
		authdata = authdata[idx+2:]
		break
	}
	if err = f.bw.Write(cmdClusterNodesBytes); err != nil {
		err = stackerr.ReplaceErrStack(err)
		return
	}
	if err = f.bw.Flush(); err != nil {
		err = stackerr.ReplaceErrStack(err)
		return
	}
	log.Info("Write Cluster Nodes CMD to Redis")
	var data []byte
	begin := f.br.Mark()
	for {
		err = f.br.Read()
		if err != nil {
			err = stackerr.ReplaceErrStack(err)
			return
		}
		reply := &redis.RESP{}
		if err = reply.Decode(f.br); err == bufio.ErrBufferFull {
			f.br.AdvanceTo(begin)
			continue
		} else if err != nil {
			err = stackerr.ReplaceErrStack(err)
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
