package postgresql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/lib/pq"
	"golang.org/x/net/proxy"
)

// proxyDialer routes outbound TCP dialing through a SOCKS proxy from the
// environment, if any. Also satisfies driver.Driver for pq.DialOpen.
type proxyDialer struct{}

func (d proxyDialer) Open(name string) (driver.Conn, error) {
	return pq.DialOpen(d, name)
}

func (d proxyDialer) Dial(network, address string) (net.Conn, error) {
	dialer := proxy.FromEnvironment()
	return dialer.Dial(network, address)
}

func (d proxyDialer) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	return proxy.Dial(ctx, network, address)
}

// proxyConnector opens lib/pq connections and optionally bounds total
// concurrent physical connections via `sem`. The semaphore is owned by the
// Config that created this connector, so aliases get independent caps.
type proxyConnector struct {
	dsn        string
	sem        chan struct{} // nil = unlimited
	semLimit   int32
	semTimeout time.Duration // 0 = wait until ctx cancellation
}

func (c *proxyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if c.sem != nil {
		start := time.Now()
		if c.semTimeout > 0 {
			select {
			case c.sem <- struct{}{}:
			case <-time.After(c.semTimeout):
				return nil, fmt.Errorf(
					"timeout acquiring connection slot (%d/%d in use); raise max_total_connections or connect_timeout",
					len(c.sem), c.semLimit,
				)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			select {
			case c.sem <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if waited := time.Since(start); waited > time.Second {
			log.Printf(
				"[DEBUG] postgresql: waited %s for connection slot (%d/%d in use)",
				waited, len(c.sem), c.semLimit,
			)
		}
	}
	conn, err := pq.DialOpen(proxyDialer{}, c.dsn)
	if err != nil {
		if c.sem != nil {
			<-c.sem
		}
		return nil, err
	}
	if c.sem == nil {
		return conn, nil
	}
	sem := c.sem
	return &semConn{Conn: conn, release: func() { <-sem }}, nil
}

func (c *proxyConnector) Driver() driver.Driver {
	return proxyDialer{}
}

// semConn releases a semaphore slot on Close. lib/pq's optional driver
// interfaces (Pinger, SessionResetter, Validator, etc.) are forwarded
// explicitly because Go's interface satisfaction is static — embedding
// driver.Conn does not promote extension interfaces.
type semConn struct {
	driver.Conn
	release func()
	once    sync.Once
}

func (c *semConn) Close() error {
	defer c.once.Do(c.release)
	return c.Conn.Close()
}

func (c *semConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // fallback for drivers without ConnBeginTx
}

func (c *semConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	return c.Prepare(query)
}

func (c *semConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *semConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *semConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *semConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *semConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *semConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := c.Conn.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}
