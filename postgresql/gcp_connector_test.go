package postgresql

import "testing"

func TestIsGCPConnectionName(t *testing.T) {
	cases := map[string]bool{
		"proj:region:inst": true,
		"proj/region/inst": true,
		"db.example.com":   false,
		"proj:region":      false,
		"":                 false,
		"a::c":             false,
	}
	for host, want := range cases {
		if got := isGCPConnectionName(host); got != want {
			t.Errorf("isGCPConnectionName(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestGCPHost(t *testing.T) {
	cases := map[string]string{
		"proj/region/inst": "proj:region:inst",
		"proj:region:inst": "proj:region:inst",
		"db.example.com":   "db.example.com",
	}
	for host, want := range cases {
		if got := gcpHost(host); got != want {
			t.Errorf("gcpHost(%q) = %q, want %q", host, got, want)
		}
	}
}
