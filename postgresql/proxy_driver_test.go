package postgresql

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input       string
		expectHosts []string
		expectPort  string
		expectErr   bool
	}{
		{
			input:       "host1:5432",
			expectHosts: []string{"host1"},
			expectPort:  "5432",
			expectErr:   false,
		},
		{
			input:       "host1,host2:5432",
			expectHosts: []string{"host1", "host2"},
			expectPort:  "5432",
			expectErr:   false,
		},
		{
			input:       "[::1]:5432",
			expectHosts: []string{"::1"}, // net.SplitHostPort strips brackets
			expectPort:  "5432",
			expectErr:   false,
		},
		{
			input:       "[::1],localhost:5432",
			expectHosts: []string{"::1", "localhost"}, // manual split strips brackets
			expectPort:  "5432",
			expectErr:   false,
		},
		{
			input:       "host1,[::1]:5432",
			expectHosts: []string{"host1", "::1"},
			expectPort:  "5432",
			expectErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hosts, port, err := parseAddress(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectHosts, hosts)
				assert.Equal(t, tt.expectPort, port)
			}
		})
	}
}

func TestReconstruction(t *testing.T) {
	// Verify that net.JoinHostPort reconstructs correctly from what parseAddress returns
	tests := []string{
		"host1:5432",
		"host1,host2:5432",
		"[::1]:5432",
		"[::1],localhost:5432",
	}

	for _, input := range tests {
		hosts, port, err := parseAddress(input)
		assert.NoError(t, err)
		for _, h := range hosts {
			addr := net.JoinHostPort(h, port)
			// Sanity check on address format
			_, _, err := net.SplitHostPort(addr)
			assert.NoError(t, err, "JoinHostPort produced invalid address: %s", addr)
		}
	}
}

