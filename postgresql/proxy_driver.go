package postgresql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
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
	return d.DialTimeout(network, address, 0)
}

func (d proxyDriver) DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	// Only handle TCP networks for multi-host splitting
	if !strings.HasPrefix(network, "tcp") {
		return proxy.Dial(ctx, network, address)
	}

	hosts, port, err := parseAddress(address)
	if err != nil {
		// If parsing fails, fall back to trying the original address
		return proxy.Dial(ctx, network, address)
	}

	var lastErr error
	for _, host := range hosts {
		addr := net.JoinHostPort(host, port)
		conn, err := proxy.Dial(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err

		// Check if context expired
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no hosts available")
}

func parseAddress(address string) ([]string, string, error) {
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		if strings.Contains(host, ",") {
			return strings.Split(host, ","), port, nil
		}
		return []string{host}, port, nil
	}

	// Fallback for when net.SplitHostPort fails (e.g. mixed bracketed and unbracketed hosts)
	lastColon := strings.LastIndex(address, ":")
	if lastColon == -1 {
		return nil, "", err
	}

	port = address[lastColon+1:]
	hostPart := address[:lastColon]

	if strings.Contains(hostPart, ",") {
		hosts := strings.Split(hostPart, ",")
		// Clean up brackets if present so net.JoinHostPort doesn't double them
		for i, h := range hosts {
			if len(h) > 2 && h[0] == '[' && h[len(h)-1] == ']' {
				hosts[i] = h[1 : len(h)-1]
			}
		}
		return hosts, port, nil
	}

	return nil, "", err
}

func init() {
	sql.Register(proxyDriverName, proxyDriver{})
}
