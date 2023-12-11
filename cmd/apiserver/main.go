package main

import (
	"flag"

	"github.com/BurntSushi/toml"

	"github.com/Hoverhuang-er/overlord/pkg/log"
	"github.com/Hoverhuang-er/overlord/platform/api/model"
	"github.com/Hoverhuang-er/overlord/platform/api/server"
	"github.com/Hoverhuang-er/overlord/platform/api/service"
	"github.com/Hoverhuang-er/overlord/version"
)

var (
	confPath string
)

func main() {
	flag.StringVar(&confPath, "conf", "conf.toml", "scheduler conf")
	flag.Parse()

	if version.ShowVersion() {
		return
	}

	conf := new(model.ServerConfig)
	_, err := toml.DecodeFile(confPath, &conf)
	if err != nil {
		panic(err)
	}
	if log.Init(conf.Config) {
		defer log.Close()
	}
	svc := service.New(conf)
	server.Run(conf, svc)
}
