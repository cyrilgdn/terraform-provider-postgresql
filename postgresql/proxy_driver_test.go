package postgresql

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeConn implements every optional driver interface lib/pq exposes so
// semConn forwarding can be verified.
type fakeConn struct {
	closeErr error
	closed   atomic.Bool

	pingCalls    atomic.Int32
	resetCalls   atomic.Int32
	isValidVal   bool
	beginTxCalls atomic.Int32
	prepCtxCalls atomic.Int32
	execCalls    atomic.Int32
	queryCalls   atomic.Int32
	checkCalls   atomic.Int32
}

func (f *fakeConn) Prepare(q string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (f *fakeConn) Close() error {
	f.closed.Store(true)
	return f.closeErr
}
func (f *fakeConn) Begin() (driver.Tx, error) { return nil, errors.New("use BeginTx") }
func (f *fakeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	f.beginTxCalls.Add(1)
	return nil, nil
}
func (f *fakeConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	f.prepCtxCalls.Add(1)
	return nil, nil
}
func (f *fakeConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	f.execCalls.Add(1)
	return nil, nil
}
func (f *fakeConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	f.queryCalls.Add(1)
	return nil, nil
}
func (f *fakeConn) Ping(ctx context.Context) error {
	f.pingCalls.Add(1)
	return nil
}
func (f *fakeConn) ResetSession(ctx context.Context) error {
	f.resetCalls.Add(1)
	return nil
}
func (f *fakeConn) IsValid() bool { return f.isValidVal }
func (f *fakeConn) CheckNamedValue(nv *driver.NamedValue) error {
	f.checkCalls.Add(1)
	return nil
}

// Compile-time assertions that fakeConn satisfies the expected interfaces.
var _ driver.Conn = (*fakeConn)(nil)
var _ driver.ConnBeginTx = (*fakeConn)(nil)
var _ driver.ConnPrepareContext = (*fakeConn)(nil)
var _ driver.ExecerContext = (*fakeConn)(nil)
var _ driver.QueryerContext = (*fakeConn)(nil)
var _ driver.Pinger = (*fakeConn)(nil)
var _ driver.SessionResetter = (*fakeConn)(nil)
var _ driver.Validator = (*fakeConn)(nil)
var _ driver.NamedValueChecker = (*fakeConn)(nil)

func TestSemConn_ForwardsAllInterfaces(t *testing.T) {
	fc := &fakeConn{isValidVal: true}
	released := atomic.Bool{}
	sc := &semConn{Conn: fc, release: func() { released.Store(true) }}

	_, _ = sc.BeginTx(context.Background(), driver.TxOptions{})
	_, _ = sc.PrepareContext(context.Background(), "SELECT 1")
	_, _ = sc.ExecContext(context.Background(), "SELECT 1", nil)
	_, _ = sc.QueryContext(context.Background(), "SELECT 1", nil)
	_ = sc.Ping(context.Background())
	_ = sc.ResetSession(context.Background())
	_ = sc.CheckNamedValue(&driver.NamedValue{})
	assert.True(t, sc.IsValid())

	assert.EqualValues(t, 1, fc.beginTxCalls.Load(), "BeginTx forwarded")
	assert.EqualValues(t, 1, fc.prepCtxCalls.Load(), "PrepareContext forwarded")
	assert.EqualValues(t, 1, fc.execCalls.Load(), "ExecContext forwarded")
	assert.EqualValues(t, 1, fc.queryCalls.Load(), "QueryContext forwarded")
	assert.EqualValues(t, 1, fc.pingCalls.Load(), "Ping forwarded")
	assert.EqualValues(t, 1, fc.resetCalls.Load(), "ResetSession forwarded")
	assert.EqualValues(t, 1, fc.checkCalls.Load(), "CheckNamedValue forwarded")
}

func TestSemConn_CloseReleasesOnce(t *testing.T) {
	fc := &fakeConn{}
	releases := atomic.Int32{}
	sc := &semConn{Conn: fc, release: func() { releases.Add(1) }}

	assert.NoError(t, sc.Close())
	assert.NoError(t, sc.Close())
	assert.True(t, fc.closed.Load())
	assert.EqualValues(t, 1, releases.Load(), "release must fire exactly once")
}

func TestSemConn_CloseReleasesEvenOnCloseError(t *testing.T) {
	fc := &fakeConn{closeErr: errors.New("forced close error")}
	released := atomic.Bool{}
	sc := &semConn{Conn: fc, release: func() { released.Store(true) }}

	err := sc.Close()
	assert.Error(t, err)
	assert.True(t, released.Load(), "semaphore must be released even when underlying Close fails")
}

// Without these assertions, database/sql silently loses optional driver
// features (Ping, ResetSession, IsValid, ...) on the wrapped conn.
func TestSemConn_SatisfiesOptionalDriverInterfaces(t *testing.T) {
	var c driver.Conn = &semConn{Conn: &fakeConn{}, release: func() {}}

	_, ok := c.(driver.ConnBeginTx)
	assert.True(t, ok, "driver.ConnBeginTx not satisfied")
	_, ok = c.(driver.ConnPrepareContext)
	assert.True(t, ok, "driver.ConnPrepareContext not satisfied")
	_, ok = c.(driver.ExecerContext)
	assert.True(t, ok, "driver.ExecerContext not satisfied")
	_, ok = c.(driver.QueryerContext)
	assert.True(t, ok, "driver.QueryerContext not satisfied")
	_, ok = c.(driver.Pinger)
	assert.True(t, ok, "driver.Pinger not satisfied")
	_, ok = c.(driver.SessionResetter)
	assert.True(t, ok, "driver.SessionResetter not satisfied")
	_, ok = c.(driver.Validator)
	assert.True(t, ok, "driver.Validator not satisfied")
	_, ok = c.(driver.NamedValueChecker)
	assert.True(t, ok, "driver.NamedValueChecker not satisfied")
}

func TestProxyConnector_AcquireTimeout(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	defer func() { <-sem }()

	c := &proxyConnector{
		dsn:        "postgres://x:y@127.0.0.1:1/postgres?sslmode=disable",
		sem:        sem,
		semLimit:   1,
		semTimeout: 50 * time.Millisecond,
	}

	start := time.Now()
	_, err := c.Connect(context.Background())
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout acquiring connection slot")
	assert.Less(t, elapsed, 500*time.Millisecond, "should have timed out quickly")
}

func TestProxyConnector_RespectsContextCancellation(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	defer func() { <-sem }()

	c := &proxyConnector{
		dsn:      "postgres://x:y@127.0.0.1:1/postgres?sslmode=disable",
		sem:      sem,
		semLimit: 1,
		// no semTimeout — only ctx-based cancellation
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Connect(ctx)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestProxyConnector_NilSemaphoreUnbounded(t *testing.T) {
	c := &proxyConnector{
		dsn: "postgres://x:y@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1",
		sem: nil,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.Connect(ctx)
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "timeout acquiring connection slot")
}

// Two connectors with independent semaphores must not interfere — that's
// what gives provider aliases independent caps.
func TestProxyConnector_IsolatedAcrossConfigs(t *testing.T) {
	semA := make(chan struct{}, 1)
	semA <- struct{}{} // saturate A
	defer func() { <-semA }()

	semB := make(chan struct{}, 1)
	a := &proxyConnector{dsn: "x", sem: semA, semLimit: 1, semTimeout: 50 * time.Millisecond}
	b := &proxyConnector{dsn: "x", sem: semB, semLimit: 1, semTimeout: 50 * time.Millisecond}

	// A should timeout on semaphore (saturated).
	_, errA := a.Connect(context.Background())
	assert.Error(t, errA)
	assert.Contains(t, errA.Error(), "timeout acquiring connection slot")

	// B has its own free semaphore — it should NOT hit the semaphore timeout;
	// it'll fail on dial instead.
	_, errB := b.Connect(context.Background())
	assert.Error(t, errB)
	assert.NotContains(t, errB.Error(), "timeout acquiring connection slot")
}
