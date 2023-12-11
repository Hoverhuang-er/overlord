#!/bin/sh

/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/clusters
/data/etcd/etcdctl mkdir /ovelord/instances
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/heartbeat
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/config
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/jobs
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/job_detail
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/framework
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/appids
/data/etcd/etcdctl mkdir /github.com/Hoverhuang-er/overlord/specs
/data/etcd/etcdctl set /github.com/Hoverhuang-er/overlord/fs "http://172.22.20.48:20080"
