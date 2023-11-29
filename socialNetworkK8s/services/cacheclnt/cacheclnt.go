package cacheclnt

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"sync"
	"sync/atomic"
	"socialnetworkk8/dialer"
	cached "socialnetworkk8/services/cached/proto"
	"github.com/bradfitz/gomemcache/memcache"
)

const (
	CACHE_CLNT_PORT = ":9999"
	N_RPC_SESSIONS = 10
)

type Selector struct {
	idx int32
	limit int32
}

func MakeSelector(limit int32) *Selector {
	return &Selector{idx: 0, limit: limit}
}

func (s *Selector) Next() int32 {
	return atomic.AddInt32(&s.idx, 1) % s.limit
}

type CacheClnt struct {
	mu  sync.Mutex
	ccs [][]cached.CachedClient
	ncs int32
	selector *Selector
}

func MakeCacheClnt() *CacheClnt {
	c := &CacheClnt{
		ccs: make([][]cached.CachedClient, 0),
		selector: MakeSelector(N_RPC_SESSIONS),
	}

	c.startRPCServer()

	return c
}

func (c *CacheClnt) Get(ctx context.Context, key string) (*memcache.Item, error) {
	if c.ncs == 0 {
		return nil, fmt.Errorf("No caches registered")
	}
	n := c.key2shard(key)
	req := cached.GetRequest{
		Key: key,
	}
	res, err := c.ccs[n][c.selector.Next()].Get(ctx, &req)
	if err != nil {
		log.Printf("Error cacheclnt get: %v", err)
	}
	if res.Ok {
		return &memcache.Item{Key: key, Value: res.Val}, nil
	}
	return nil, memcache.ErrCacheMiss
}

func (c *CacheClnt) Set(ctx context.Context, item *memcache.Item) bool {
	n := c.key2shard(item.Key)
	req := cached.SetRequest{
		Key: item.Key,
		Val: item.Value,
	}
	res, err := c.ccs[n][c.selector.Next()].Set(ctx, &req)
	if err != nil {
		log.Printf("Error cacheclnt set: %v", err)
	}
	return res.Ok
}

func (c *CacheClnt) Delete(ctx context.Context, key string) bool {
	n := c.key2shard(key)
	req := cached.DeleteRequest{Key: key}
	res, err := c.ccs[n][c.selector.Next()].Delete(ctx, &req)
	if err != nil {
		log.Printf("Error cacheclnt delete: %v", err)
	}
	return res.Ok
}

type RegisterCacheRequest struct {
	Addr string
}

type RegisterCacheResponse struct {
	OK bool
}

func (c *CacheClnt) RegisterCache(req *RegisterCacheRequest, rep *RegisterCacheResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("Registering new cache server %v", req.Addr)
	rep.OK = true

	// Make a deep copy of the client slice, so we can atomically swap it with
	// the existing slice. This way, clients don't have to take a lock on the
	// slice when executing RPCs.
	ccs := make([][]cached.CachedClient, len(c.ccs))
	for i := range ccs {
		ccs[i] = make([]cached.CachedClient, N_RPC_SESSIONS)
		for j := range ccs[i] {
			ccs[i][j] = c.ccs[i][j]
		}
	}
	// Append the new client.
	ccs = append(ccs, dialClient(req.Addr))
	// Swap in the new slice, which should be done atomically.
	c.ccs = ccs
	// Atomically increase the number by which we mod when selecting a shard.
	atomic.AddInt32(&c.ncs, 1)
	log.Printf("Done registering new cache server %v", req.Addr)
	return nil
}

func (c *CacheClnt) startRPCServer() {
	rpc.Register(c)
	rpc.HandleHTTP()
	l, err := net.Listen("tcp", CACHE_CLNT_PORT)
	if err != nil {
		log.Fatalf("Error Listen in Coordinator.registerServer: %v", err)
	}
	go http.Serve(l, nil)
}

func (c *CacheClnt) key2shard(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	shard := int(h.Sum32()) % int(atomic.LoadInt32(&c.ncs))
	return shard
}

func dialClient(addr string) []cached.CachedClient {
	// Dial the new server
	// XXX fix
	clients := make([]cached.CachedClient, N_RPC_SESSIONS)
	for i := range clients {
		conn, err := dialer.Diall(i, addr, nil)
		if err != nil {
			log.Fatalf("Error dial cachesrv: %v", err)
		}
		clients[i] = cached.NewCachedClient(conn)
	}
	return clients
}

