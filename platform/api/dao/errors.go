package dao

import "errors"

// define stackerr
var (
	ErrMasterNumMustBeEven = errors.New("master number must be even")
	ErrCacheTypeNotSupport = errors.New("cache type only support memcache|redis|redis_cluster")
)
