package main

import (
	"flag"
	"strings"

	"github.com/Hoverhuang-er/overlord/pkg/etcd"
	"github.com/Hoverhuang-er/overlord/pkg/log"
	"github.com/Hoverhuang-er/overlord/platform/job/balance"
	"github.com/Hoverhuang-er/overlord/version"
)

var (
	cluster string
	db      string
)

func main() {
	flag.StringVar(&cluster, "cluster", "", "cluster name")
	flag.StringVar(&db, "db", "", "etcd dsn")
	flag.Parse()
	if version.ShowVersion() {
		return
	}

	log.InitHandle(log.NewStdHandler())
	var etcdURL string
	if strings.HasPrefix(db, "http://") {
		etcdURL = db
	} else {
		etcdURL = "http://" + db
	}

	e, err := etcd.New(etcdURL)
	if err != nil {
		log.Errorf("balance fail to connect to etcd due %v", err)
		return
	}

	err = balance.Balance(cluster, e)
	if err != nil {
		log.Errorf("fail to init balance %s job due %v", cluster, err)
		return
	}
}
