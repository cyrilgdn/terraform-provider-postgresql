package postgresql

import (
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
