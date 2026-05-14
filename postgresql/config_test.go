package postgresql

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/blang/semver"
	"github.com/stretchr/testify/assert"
)

func TestConfigConnParams(t *testing.T) {
	var tests = []struct {
		input *Config
		want  []string
	}{
		{&Config{Scheme: "postgres", SSLMode: "require", ConnectTimeoutSec: 10}, []string{"connect_timeout=10", "sslmode=require"}},
		{&Config{Scheme: "postgres", SSLMode: "disable"}, []string{"connect_timeout=0", "sslmode=disable"}},
		{&Config{Scheme: "awspostgres", ConnectTimeoutSec: 10}, []string{}},
		{&Config{Scheme: "awspostgres", SSLMode: "disable"}, []string{}},
		{&Config{ExpectedVersion: semver.MustParse("9.0.0"), ApplicationName: "Terraform provider"}, []string{"fallback_application_name=Terraform+provider"}},
		{&Config{ExpectedVersion: semver.MustParse("8.0.0"), ApplicationName: "Terraform provider"}, []string{}},
		{&Config{SSLClientCert: &ClientCertificateConfig{CertificatePath: "/path/to/public-certificate.pem", KeyPath: "/path/to/private-key.pem"}}, []string{"sslcert=%2Fpath%2Fto%2Fpublic-certificate.pem", "sslkey=%2Fpath%2Fto%2Fprivate-key.pem"}},
		{&Config{SSLRootCertPath: "/path/to/root.pem"}, []string{"sslrootcert=%2Fpath%2Fto%2Froot.pem"}},
	}

	for _, test := range tests {

		connParams := test.input.connParams()

		sort.Strings(connParams)
		sort.Strings(test.want)

		if !reflect.DeepEqual(connParams, test.want) {
			t.Errorf("Config.connParams(%+v) returned %#v, want %#v", test.input, connParams, test.want)
		}

	}
}

// Provider aliases must not accidentally share pool cache.
func TestConnect_RegistryIsPerConfig(t *testing.T) {
	configA := &Config{poolRegistry: &sync.Map{}}
	configB := &Config{poolRegistry: &sync.Map{}}

	sentinel := &registryEntry{}
	configA.poolRegistry.Store("dsn://x", sentinel)

	gotA, okA := configA.poolRegistry.Load("dsn://x")
	gotB, okB := configB.poolRegistry.Load("dsn://x")

	assert.True(t, okA)
	assert.Same(t, sentinel, gotA)
	assert.False(t, okB)
	assert.Nil(t, gotB)
}

// External callers using `Config{...}.NewClient().Connect()` must work even
// without providerConfigure setting poolRegistry.
func TestConfig_LazyRegistryInitFromNewClient(t *testing.T) {
	cfg := &Config{Scheme: "postgres"}
	assert.Nil(t, cfg.poolRegistry)

	client := cfg.NewClient("db")
	assert.NotNil(t, cfg.poolRegistry)
	assert.Same(t, cfg.poolRegistry, client.config.poolRegistry)

	client2 := cfg.NewClient("db2")
	assert.Same(t, cfg.poolRegistry, client2.config.poolRegistry)
}

func TestConfig_LazyRegistryInitConcurrent(t *testing.T) {
	cfg := &Config{Scheme: "postgres"}
	const N = 50
	results := make(chan *sync.Map, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- cfg.NewClient("db").config.poolRegistry
		}()
	}
	wg.Wait()
	close(results)
	first := <-results
	assert.NotNil(t, first)
	for r := range results {
		assert.Same(t, first, r)
	}
}

// Dial-failure path: pq.DialOpen fails at the TCP layer, proxyConnector's
// own error branch releases the slot. Does not exercise openAndPing's
// db.Close — see TestConnect_PostHandshakeFailureReleasesSemaphoreSlot.
func TestConnect_DialFailureReleasesSemaphoreSlot(t *testing.T) {
	const slots = 2
	cfg := &Config{
		Scheme:            "postgres",
		Host:              "127.0.0.1",
		Port:              1,
		Username:          "u",
		Password:          "p",
		SSLMode:           "disable",
		ConnectTimeoutSec: 1,
		MaxConns:          5,
		MaxTotalConns:     slots,
		ExpectedVersion:   semver.MustParse("9.0.0"),
	}

	for i := 0; i < slots*3; i++ {
		client := cfg.NewClient("postgres")
		_, err := client.Connect()
		assert.Error(t, err, "attempt %d", i)
		assert.Equal(t, 0, len(cfg.serverSem), "attempt %d: slot leaked", i)
	}
}

// Post-handshake path: pq.DialOpen succeeds, db.Ping then fails because the
// server hung up. Slot release here depends on openAndPing's db.Close.
func TestConnect_PostHandshakeFailureReleasesSemaphoreSlot(t *testing.T) {
	host, port, stop := startFakePostgresServerCompleteThenClose(t)
	defer stop()

	const slots = 2
	cfg := &Config{
		Scheme:            "postgres",
		Host:              host,
		Port:              port,
		Username:          "u",
		Password:          "p",
		SSLMode:           "disable",
		ConnectTimeoutSec: 2,
		MaxConns:          5,
		MaxTotalConns:     slots,
		ExpectedVersion:   semver.MustParse("9.0.0"),
	}

	for i := 0; i < slots*3; i++ {
		client := cfg.NewClient("postgres")
		_, err := client.Connect()
		assert.Error(t, err, "attempt %d", i)
		assert.Equal(t, 0, len(cfg.serverSem), "attempt %d: slot leaked", i)
	}
}

// Accepts a connection, completes the Postgres startup handshake
// (AuthenticationOk + BackendKeyData + ReadyForQuery), then hangs up — the
// client's next query (Ping's simpleQuery) hits EOF.
func startFakePostgresServerCompleteThenClose(t *testing.T) (host string, port int, stop func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake postgres listener: %v", err)
	}

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()

				// Read StartupMessage. First 4 bytes = length (incl. itself).
				lenBuf := make([]byte, 4)
				if _, err := io.ReadFull(c, lenBuf); err != nil {
					return
				}
				bodyLen := int(binary.BigEndian.Uint32(lenBuf)) - 4
				if bodyLen < 0 || bodyLen > 10000 {
					return
				}
				if _, err := io.ReadFull(c, make([]byte, bodyLen)); err != nil {
					return
				}

				// AuthenticationOk + BackendKeyData + ReadyForQuery, then hang up.
				var buf bytes.Buffer
				buf.WriteByte('R')
				_ = binary.Write(&buf, binary.BigEndian, int32(8))
				_ = binary.Write(&buf, binary.BigEndian, int32(0))
				buf.WriteByte('K')
				_ = binary.Write(&buf, binary.BigEndian, int32(12))
				_ = binary.Write(&buf, binary.BigEndian, int32(1))
				_ = binary.Write(&buf, binary.BigEndian, int32(1))
				buf.WriteByte('Z')
				_ = binary.Write(&buf, binary.BigEndian, int32(5))
				buf.WriteByte('I')
				_, _ = c.Write(buf.Bytes())
			}(c)
		}
	}()

	addr := l.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { _ = l.Close() }
}

func TestConfigConnStr(t *testing.T) {
	var tests = []struct {
		input        *Config
		wantDbURL    string
		wantDbParams []string
	}{
		{&Config{Scheme: "postgres", Host: "localhost", Port: 5432, Username: "postgres_user", Password: "postgres_password", SSLMode: "disable"}, "postgres://postgres_user:postgres_password@localhost:5432/postgres", []string{"connect_timeout=0", "sslmode=disable"}},
		{&Config{Scheme: "postgres", Host: "localhost", Port: 5432, Username: "spaced user", Password: "spaced password", SSLMode: "disable"}, "postgres://spaced%20user:spaced%20password@localhost:5432/postgres", []string{"connect_timeout=0", "sslmode=disable"}},
	}

	for _, test := range tests {

		connStr := test.input.connStr("postgres")

		splitConnStr := strings.Split(connStr, "?")

		if splitConnStr[0] != test.wantDbURL {
			t.Errorf("Config.connStr(%+v) returned %#v, want %#v", test.input, splitConnStr[0], test.wantDbURL)
		}

		connParams := strings.Split(splitConnStr[1], "&")

		sort.Strings(connParams)
		sort.Strings(test.wantDbParams)

		if !reflect.DeepEqual(connParams, test.wantDbParams) {
			t.Errorf("Config.connStr(%+v) returned %#v, want %#v", test.input, connParams, test.wantDbParams)
		}

	}
}
