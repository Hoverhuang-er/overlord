package etcd

import (
	"context"
	"encoding/json"
	"github.com/Hoverhuang-er/overlord/pkg/types"
	"testing"

	"github.com/Hoverhuang-er/overlord/platform/job"

	"github.com/stretchr/testify/assert"
)

func TestEtcd(t *testing.T) {
	e, err := New("http://127.0.0.1:2379")
	ctx := context.TODO()
	assert.NoError(t, err)
	_, _ = e.GenID(ctx, "/order", "1")
	_, _ = e.GenID(ctx, "/order", "2")
	_, err = e.Get(ctx, "/order")
	assert.NoError(t, err)
}
func TestSet(t *testing.T) {
	e, err := New("http://127.0.0.1:2379")
	assert.NoError(t, err)
	ctx := context.TODO()
	assert.NoError(t, err)
	mcjob := job.Job{
		Name:      "test",
		OpType:    job.OpCreate,
		CacheType: types.CacheTypeMemcache,
		Version:   "1.5.12",
		Num:       6,
		MaxMem:    10,
		CPU:       0.1,
	}
	bs, err := json.Marshal(mcjob)
	assert.NoError(t, err)
	err = e.Set(ctx, "/github.com/Hoverhuang-er/overlord/jobs/job1", string(bs))
	assert.NoError(t, err)

	// redisjob := &job.Job{
	// 	Name:      "test",
	// 	CacheType: types.CacheTypeRedis,
	// 	Version:   "4.0.11",
	// 	Num:       6,
	// 	MaxMem:    10,
	// 	CPU:       0.1,
	// }
	// bs, err = json.Marshal(redisjob)
	// assert.NoError(t, err)
	// err = e.Set(ctx, "/github.com/Hoverhuang-er/overlord/jobs/job12", string(bs))
	// assert.NoError(t, err)
}

func TestSequnenceOk(t *testing.T) {
	e, err := New("http://127.0.0.1:2379")
	assert.NoError(t, err)
	ctx := context.Background()
	_ = e.Delete(ctx, PortSequence)

	port, err := e.Sequence(ctx, PortSequence)
	assert.NoError(t, err)
	assert.Equal(t, int64(20000), port)

	port, err = e.Sequence(ctx, PortSequence)
	assert.NoError(t, err)
	assert.Equal(t, int64(20001), port)
}
