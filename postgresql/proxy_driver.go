package postgresql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net"
	"time"

	"github.com/lib/pq"
	"golang.org/x/net/proxy"
)

const proxyDriverName = "postgresql-proxy"

type proxyDriver struct{}

func (d proxyDriver) Open(name string) (driver.Conn, error) {
	return pq.DialOpen(d, name)
}

func (d proxyDriver) Dial(network, address string) (net.Conn, error) {
	dialer := proxy.FromEnvironment()
	return dialer.Dial(network, address)
}

func (d proxyDriver) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()
	return proxy.Dial(ctx, network, address)
}

func init() {
	sql.Register(proxyDriverName, proxyDriver{})
}
