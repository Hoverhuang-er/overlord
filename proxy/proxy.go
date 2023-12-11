package proxy

import (
	errs "errors"
	"github.com/Hoverhuang-er/overlord/pkg/stackerr"
	"net"
	"path/filepath"
	"sort"
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

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
)

// proxy stackerr
var (
	ErrProxyMoreMaxConns = errs.New("Proxy accept more than max connextions")
	ErrProxyReloadIgnore = errs.New("Proxy reload cluster config is ignored")
	ErrProxyReloadFail   = errs.New("Proxy reload cluster config is failed")
)

// Proxy is proxy.
type Proxy struct {
	c   *Config
	ccf string // cluster configure file name
	ccs []*ClusterConfig

	forwarders map[string]proto.Forwarder
	lock       sync.Mutex

	conns int32

	closed bool
}

// New new a proxy by config.
func New(c *Config) (p *Proxy, err error) {
	if err = c.Validate(); err != nil {
		return nil, stackerr.ReplaceErrStack(err)
	}
	p = &Proxy{}
	p.c = c
	return
}

// Serve is the main accept() loop of a server.
func (p *Proxy) Serve(ccs []*ClusterConfig) {
	p.ccs = ccs
	if len(ccs) == 0 {
		log.Warn("overlord will never listen on any port due to cluster is not specified")
	}
	p.lock.Lock()
	p.forwarders = map[string]proto.Forwarder{}
	p.lock.Unlock()
	for _, cc := range ccs {
		log.Infof("start to serve cluster[%s] with configs %v \n nodeAddr:%s", cc.Name, *cc, cc.ListenAddr)
		p.serve(cc)
	}
}

func (p *Proxy) serve(cc *ClusterConfig) {
	forwarder := NewForwarder(cc)
	p.forwarders[cc.Name] = forwarder
	// listen
	l, err := Listen(cc.ListenProto, cc.ListenAddr)
	if err != nil {
		panic(err)
	}
	log.Infof("overlord proxy cluster[%s] addr(%s) start listening", cc.Name, cc.ListenAddr)
	if cc.SlowlogSlowerThan != 0 {
		log.Infof("overlord start slowlog to [%s] with threshold [%d]us", cc.Name, cc.SlowlogSlowerThan)
	}
	if cc.ToRedis.Enable {
		if p.checkConnect2Redis(cc) != nil {
			log.Errorf("To Redis Auth Enabled, overlord connect  cluster[%s] addr(%s) connect to redis failed", cc.Name, cc.ListenAddr)
			return
		}
		log.Infof("overlord proxy cluster[%s] addr(%s) enabled to redis", cc.Name, cc.ListenAddr)
		if cc.ToProxy.Enable {
			if p.checkConnect2Proxy(cc) != nil {
				log.Errorf("To Proxy Auth Enabled, overlord proxy cluster[%s] addr(%s) connect to proxy failed", cc.Name, cc.ListenAddr)
				return
			}
			log.Infof("overlord proxy cluster[%s] addr(%s) enabled to proxy", cc.Name, cc.ListenAddr)
		}
		go p.acceptV2(cc, l, forwarder)
	} else {
		go p.accept(cc, l, forwarder)
	}

}

// check connect to redis auth if enabled
func (p *Proxy) checkConnect2Redis(cc *ClusterConfig) error {
	if cc.ToRedis.Auth.Password == "" && len(cc.ToProxy.Auth.Password) == 0 {
		return errors.New("redis auth password is empty, please check config file")
	}
	return nil
}

// check connect to proxy auth if enabled
func (p *Proxy) checkConnect2Proxy(cc *ClusterConfig) error {
	if cc.ToProxy.Auth.Password == "" && len(cc.ToProxy.Auth.Password) == 0 {
		return errors.New("proxy auth password is empty, please check config file")
	}
	return nil
}

func (p *Proxy) accept(cc *ClusterConfig, l net.Listener, forwarder proto.Forwarder) {
	for {
		if p.closed {
			log.Infof("overlord proxy cluster[%s] addr(%s) stop listen", cc.Name, cc.ListenAddr)
			return
		}
		conn, err := l.Accept()
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			log.Errorf("cluster(%s) addr(%s) accept connection error:%+v", cc.Name, cc.ListenAddr, err)
			continue
		}
		if p.c.Proxy.MaxConnections > 0 {
			if conns := atomic.LoadInt32(&p.conns); conns > p.c.Proxy.MaxConnections {
				// cache type
				var encoder proto.ProxyConn
				switch cc.CacheType {
				case types.CacheTypeMemcache:
					encoder = memcache.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second))
				case types.CacheTypeMemcacheBinary:
					encoder = mcbin.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second))
				case types.CacheTypeRedis:
					encoder = redis.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second), cc.Password)
					//encoder = redis.NewProxyConnV2(libnet.NewConn(conn, time.Second, time.Second), cc.ToRedis.Auth.CaFile, cc.ToRedis.Auth.CertFile, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)
				case types.CacheTypeRedisCluster:
					encoder = rclstr.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second), nil, cc.Password)
					//encoder = rclstr.NewProxyConnV2(libnet.NewConn(conn, time.Second, time.Second), nil, cc.ToRedis.Auth.CaFile, cc.ToRedis.Auth.CertFile, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)
				}
				if encoder != nil {
					_ = encoder.Encode(proto.ErrMessage(ErrProxyMoreMaxConns))
					_ = encoder.Flush()
				}
				_ = conn.Close()
				log.Warnf("proxy reject connection count(%d) due to more than max(%d)", conns, p.c.Proxy.MaxConnections)
				continue
			}
		}
		atomic.AddInt32(&p.conns, 1)
		NewHandler(p, cc, conn, forwarder).Handle()
	}
}

// Acceptv2
func (p *Proxy) acceptV2(cc *ClusterConfig, l net.Listener, forwarder proto.Forwarder) {
	if err := p.verifyToProxyPassword(cc); err != nil {
		log.Errorf("cluster(%s) addr(%s) verify connection error:%+v", cc.Name, cc.ListenAddr, err)
		return
	}
	log.Infof("cluster(%s) addr(%s) verify connection successful", cc.Name, cc.ListenAddr)
	for {
		if p.closed {
			log.Warnf("overlord proxy cluster[%s] addr(%s) stop listen", cc.Name, cc.ListenAddr)
			return
		}
		conn, err := l.Accept()
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			log.Errorf("cluster(%s) addr(%s) accept connection error:%+v", cc.Name, cc.ListenAddr, err)
			continue
		}
		if p.c.Proxy.MaxConnections > 0 {
			if conns := atomic.LoadInt32(&p.conns); conns > p.c.Proxy.MaxConnections {
				// cache type
				var encoder proto.ProxyConn
				switch cc.CacheType {
				case types.CacheTypeMemcache:
					encoder = memcache.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second))
				case types.CacheTypeMemcacheBinary:
					encoder = mcbin.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second))
				case types.CacheTypeRedis:
					//encoder = redis.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second), cc.Password)
					encoder = redis.NewProxyConnV2(libnet.NewConn(conn, time.Second, time.Second), cc.ToRedis.Auth.CaFile, cc.ToRedis.Auth.CertFile, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)
				case types.CacheTypeRedisCluster:
					//encoder = rclstr.NewProxyConn(libnet.NewConn(conn, time.Second, time.Second), nil, cc.Password, cc)
					encoder = rclstr.NewProxyConnV2(libnet.NewConn(conn, time.Second, time.Second), nil, cc.ToRedis.Auth.CaFile, cc.ToRedis.Auth.CertFile, cc.ToRedis.Auth.Password, cc.ToRedis.Auth.UseTLS)
				}
				if encoder != nil {
					_ = encoder.Encode(proto.ErrMessage(ErrProxyMoreMaxConns))
					_ = encoder.Flush()
				}
				_ = conn.Close()
				log.Warnf("proxy reject connection count(%d) due to more than max(%d)", conns, p.c.Proxy.MaxConnections)

				continue
			}
		}
		atomic.AddInt32(&p.conns, 1)
		NewHandler(p, cc, conn, forwarder).Handle()
	}
}

// Verify ToProxy Password
func (p *Proxy) verifyToProxyPassword(cc *ClusterConfig) error {
	if cc.ToProxy.Auth.Password == "" && len(cc.ToProxy.Auth.Password) == 0 {
		return errors.New("proxy auth password is empty, please check config file")
	}
	log.Infof("overlord proxy cluster[%s] addr(%s) connected", cc.Name, cc.ListenAddr)
	return nil
}

// Close close proxy resource.
func (p *Proxy) Close() error {
	if p.closed {
		return nil
	}
	for _, forwarder := range p.forwarders {
		forwarder.Close()
	}
	p.closed = true
	return nil
}

// MonitorConfChange reload servers.
func (p *Proxy) MonitorConfChange(ccf string) {
	p.ccf = ccf
	// start watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		log.Errorf("failed to create file change watcher and get error:%v", err)
		return
	}
	defer watch.Close()
	absPath, err := filepath.Abs(filepath.Dir(p.ccf))
	if err != nil {
		log.Errorf("failed to get abs path of file:%s and get error:%v", p.ccf, err)
		return
	}
	if err = watch.Add(absPath); err != nil {
		log.Errorf("failed to monitor content change of dir:%s with error:%v", absPath, err)
		return
	}
	log.Infof("proxy is watching changes cluster config absolute path as %s", absPath)
	for {
		if p.closed {
			log.Warnf("proxy is closed and exit configure file:%s monitor", p.ccf)
			return
		}
		select {
		case ev := <-watch.Events:
			if ev.Op&fsnotify.Create == fsnotify.Create || ev.Op&fsnotify.Write == fsnotify.Write || ev.Op&fsnotify.Rename == fsnotify.Rename {
				time.Sleep(time.Second)
				newConfs, err := LoadClusterConf(p.ccf)
				if err != nil {
					prom.ErrIncr(p.ccf, p.ccf, "config reload", err.Error())
					log.Errorf("failed to load conf file:%s and got error:%v", p.ccf, err)
					continue
				}
				changed := parseChanged(newConfs, p.ccs)
				for _, conf := range changed {
					if err = p.updateConfig(conf); err == nil {
						log.Infof("reload successful cluster:%s config succeed", conf.Name)
					} else {
						prom.ErrIncr(conf.Name, conf.Name, "cluster reload", err.Error())
						log.Errorf("reload failed cluster:%s config and get error:%v", conf.Name, err)
					}
				}
				log.Infof("watcher file:%s occurs event:%s and reload finish", ev.Name, ev.String())
				continue
			}
			log.Warnf("watcher file:%s occurs event:%s and ignore", ev.Name, ev.String())

		case err := <-watch.Errors:
			log.Errorf("watcher dir:%s get error:%v", absPath, err)
			return
		}
	}
}

func (p *Proxy) updateConfig(conf *ClusterConfig) (err error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	f, ok := p.forwarders[conf.Name]
	if !ok {
		err = errors.Wrapf(ErrProxyReloadIgnore, "cluster:%s", conf.Name)
		return
	}
	if err = f.Update(conf.Servers); err != nil {
		err = errors.Wrapf(ErrProxyReloadFail, "cluster:%s error:%v", conf.Name, err)
		return
	}
	for _, oldConf := range p.ccs {
		if oldConf.Name != conf.Name {
			continue
		}
		oldConf.Servers = make([]string, len(conf.Servers), cap(conf.Servers))
		copy(oldConf.Servers, conf.Servers)
		return
	}
	return
}

func parseChanged(newConfs, oldConfs []*ClusterConfig) (changed []*ClusterConfig) {

	changed = make([]*ClusterConfig, 0, len(oldConfs))
	for _, cf := range newConfs {
		sort.Strings(cf.Servers)
	}

	for _, cf := range oldConfs {
		sort.Strings(cf.Servers)
	}

	for _, newConf := range newConfs {
		for _, oldConf := range oldConfs {
			if newConf.Name != oldConf.Name {
				continue
			}

			if !deepEqualOrderedStringSlice(newConf.Servers, oldConf.Servers) {
				changed = append(changed, newConf)
			}
			break
		}
	}
	return
}

func deepEqualOrderedStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
