package main

import (
	"context"
	"flag"

	"github.com/Hoverhuang-er/overlord/pkg/log"
	"github.com/Hoverhuang-er/overlord/platform/mesos"
	"github.com/Hoverhuang-er/overlord/version"
)

func main() {
	flag.Parse()
	if version.ShowVersion() {
		return
	}

	ec := mesos.New()
	log.InitHandle(log.NewStdHandler())
	ec.Run(context.Background())
}
