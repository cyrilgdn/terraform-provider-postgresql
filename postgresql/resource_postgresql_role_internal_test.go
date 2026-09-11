package postgresql

import (
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestParseMillisecondsSetting(t *testing.T) {
	cases := []struct {
		value    string
		expected int
	}{
		{"120000", 120000},
		{"0", 0},
		{"2min", 120000},
		{"5s", 5000},
		{"150ms", 150},
		{"1h", 3600000},
		{"1d", 86400000},
		{"1500us", 2},
		{"1.5s", 1500},
		{"2 min", 120000},
	}
	for _, c := range cases {
		res, err := parseMillisecondsSetting(c.value)
		if err != nil {
			t.Errorf("parseMillisecondsSetting(%q) returned error: %v", c.value, err)
			continue
		}
		if res != c.expected {
			t.Errorf("parseMillisecondsSetting(%q) = %d, expected %d", c.value, res, c.expected)
		}
	}

	for _, invalid := range []string{"abc", "5fortnights", "", "min"} {
		if _, err := parseMillisecondsSetting(invalid); err == nil {
			t.Errorf("parseMillisecondsSetting(%q) expected error, got none", invalid)
		}
	}
}

func TestReadStatementTimeoutWithUnits(t *testing.T) {
	roleConfig := pq.ByteaArray{[]byte("statement_timeout=2min"), []byte("lock_timeout=5s")}
	res, err := readStatementTimeout(roleConfig)
	if err != nil {
		t.Fatalf("readStatementTimeout returned error: %v", err)
	}
	if res != 120000 {
		t.Errorf("readStatementTimeout = %d, expected 120000", res)
	}
}

func TestReadIdleInTransactionSessionTimeoutErrorNamesSetting(t *testing.T) {
	roleConfig := pq.ByteaArray{[]byte("idle_in_transaction_session_timeout=bogus")}
	_, err := readIdleInTransactionSessionTimeout(roleConfig)
	if err == nil {
		t.Fatal("expected error for bogus value")
	}
	if got := err.Error(); !strings.Contains(got, "idle_in_transaction_session_timeout") {
		t.Errorf("error should name the setting being read, got: %s", got)
	}
}

func TestReadLockTimeout(t *testing.T) {
	roleConfig := pq.ByteaArray{[]byte("statement_timeout=2min"), []byte("lock_timeout=5s")}
	res, err := readLockTimeout(roleConfig)
	if err != nil {
		t.Fatalf("readLockTimeout returned error: %v", err)
	}
	if res != 5000 {
		t.Errorf("readLockTimeout = %d, expected 5000", res)
	}
}
