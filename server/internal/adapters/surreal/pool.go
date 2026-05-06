package surreal

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/connection"
	"github.com/surrealdb/surrealdb.go/pkg/connection/gorillaws"

	"github.com/commit0-dev/commit0/server/internal/config"
)

// ConnPool is a round-robin pool of SurrealDB connections.
// Reads and writes use separate pools so indexing never blocks queries.
type ConnPool struct {
	conns []*surrealdb.DB
	idx   atomic.Uint64
	label string
	log   *slog.Logger
}

// NewConnPool creates a pool of `size` WebSocket connections to SurrealDB.
func NewConnPool(ctx context.Context, cfg *config.SurrealConfig, size int, label string) (*ConnPool, error) {
	if size <= 0 {
		size = 1
	}

	log := slog.Default().With("adapter", "surreal-pool", "pool", label, "size", size)
	log.Info("creating connection pool")

	u, err := url.ParseRequestURI(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("pool %s: invalid URL: %w", label, err)
	}

	rpcTimeout := defaultRPCTimeout
	if cfg.RPCTimeoutS > 0 {
		rpcTimeout = time.Duration(cfg.RPCTimeoutS) * time.Second
	}

	connectTimeout := 30 * time.Second
	if cfg.ConnectTimeoutS > 0 {
		connectTimeout = time.Duration(cfg.ConnectTimeoutS) * time.Second
	}

	pool := &ConnPool{
		conns: make([]*surrealdb.DB, 0, size),
		label: label,
		log:   log,
	}

	for i := 0; i < size; i++ {
		connCfg := connection.NewConfig(u)
		ws := gorillaws.New(connCfg).SetTimeOut(rpcTimeout)

		connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
		db, err := surrealdb.FromConnection(connCtx, ws)
		cancel()
		if err != nil {
			pool.Close(ctx)
			return nil, fmt.Errorf("pool %s conn %d: dial: %w", label, i, err)
		}

		if _, err := db.SignIn(ctx, map[string]any{"user": cfg.User, "pass": cfg.Pass}); err != nil {
			pool.Close(ctx)
			return nil, fmt.Errorf("pool %s conn %d: signin: %w", label, i, err)
		}

		if err := db.Use(ctx, cfg.Namespace, cfg.Database); err != nil {
			pool.Close(ctx)
			return nil, fmt.Errorf("pool %s conn %d: USE: %w", label, i, err)
		}

		pool.conns = append(pool.conns, db)
	}

	log.Info("pool ready", "connections", len(pool.conns))
	return pool, nil
}

// Acquire returns the next connection in round-robin order.
// This is lock-free (atomic increment).
func (p *ConnPool) Acquire() *surrealdb.DB {
	n := p.idx.Add(1)
	return p.conns[int(n-1)%len(p.conns)]
}

// Close shuts down all connections in the pool.
func (p *ConnPool) Close(ctx context.Context) {
	for i, db := range p.conns {
		if err := db.Close(ctx); err != nil {
			p.log.Warn("close pool conn", "index", i, "err", err)
		}
	}
	p.conns = nil
}

// Size returns the number of connections in the pool.
func (p *ConnPool) Size() int {
	return len(p.conns)
}
