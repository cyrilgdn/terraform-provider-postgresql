package postgresql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/lib/pq"
	"golang.org/x/net/proxy"
)

const proxyDriverName = "postgresql-proxy"

type proxyDriver struct {
	proxyURL string
}

func (d proxyDriver) Open(name string) (driver.Conn, error) {
	return pq.DialOpen(d, name)
}

func (d proxyDriver) Dial(network, address string) (net.Conn, error) {
	dialer, err := d.dialer()
	if err != nil {
		return nil, err
	}
	return dialer.Dial(network, address)
}

func (d proxyDriver) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	dialer, err := d.dialer()
	if err != nil {
		return nil, err
	}

	if xd, ok := dialer.(proxy.ContextDialer); ok {
		return xd.DialContext(ctx, network, address)
	} else {
		return nil, fmt.Errorf("unexpected protocol error")
	}
}

func (d proxyDriver) dialer() (proxy.Dialer, error) {
	proxyURL := d.proxyURL
	if proxyURL == "" {
		proxyURL = os.Getenv("PGPROXY")
	}
	if proxyURL == "" {
		return proxy.FromEnvironment(), nil
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	dialer, err := proxy.FromURL(u, proxy.Direct)
	if err != nil {
		return nil, err
	}

	noProxy := ""
	if v := os.Getenv("NO_PROXY"); v != "" {
		noProxy = v
	}
	if v := os.Getenv("no_proxy"); noProxy == "" && v != "" {
		noProxy = v
	}
	if noProxy != "" {
		perHost := proxy.NewPerHost(dialer, proxy.Direct)
		perHost.AddFromString(noProxy)

		dialer = perHost
	}

	return dialer, nil
}

type proxyConnector struct {
	dsn      string
	proxyURL string
}

var _ driver.Connector = (*proxyConnector)(nil)

func (c proxyConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.Driver().Open(c.dsn)
}

func (c proxyConnector) Driver() driver.Driver {
	return proxyDriver{c.proxyURL}
}

func init() {
	sql.Register(proxyDriverName, proxyDriver{})
}
