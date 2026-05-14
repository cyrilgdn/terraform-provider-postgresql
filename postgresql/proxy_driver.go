package postgresql

import (
	"context"
	"database/sql/driver"
	"net"
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

// proxyConnector wires lib/pq's SOCKS-aware dialer into database/sql so the
// stdlib pool can manage connections. Plain wrapper; no throttling here —
// per-database scheduling lives in dbScheduler at the resource boundary.
type proxyConnector struct {
	dsn string
}

func (c *proxyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return pq.DialOpen(proxyDialer{}, c.dsn)
}

func (c *proxyConnector) Driver() driver.Driver {
	return proxyDialer{}
}
