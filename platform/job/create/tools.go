package create

import (
	"context"
	"fmt"
	"github.com/Hoverhuang-er/overlord/pkg/etcd"
)

func cleanEtcdDirtyDir(ctx context.Context, e *etcd.Etcd, instance string) error {
	return e.RMDir(ctx, fmt.Sprintf("%s/%s", etcd.InstanceDirPrefix, instance))
}
