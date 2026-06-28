package postgresql

import (
	"strings"
	"testing"
)

func TestGCPKVQuote(t *testing.T) {
	cases := map[string]string{
		"simple":  "'simple'",
		"a b":     "'a b'",
		"o'brien": `'o\'brien'`,
		`back\sl`: `'back\\sl'`,
	}
	for in, want := range cases {
		if got := gcpKVQuote(in); got != want {
			t.Errorf("gcpKVQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGCPDSN(t *testing.T) {
	// password auth includes the password
	pw := &Config{Host: "proj/region/inst", Port: 5432, Username: "u", Password: "p", ApplicationName: "Terraform provider"}
	got := gcpDSN(pw, "mydb")
	for _, want := range []string{"host='proj:region:inst'", "port=5432", "user='u'", "dbname='mydb'", "password='p'", "application_name='Terraform provider'"} {
		if !strings.Contains(got, want) {
			t.Errorf("gcpDSN password auth = %q, missing %q", got, want)
		}
	}

	// IAM auth omits the password even when set
	iam := &Config{Host: "proj:region:inst", Port: 5432, Username: "sa@proj.iam", Password: "ignored", GCPIAMAuth: true}
	got = gcpDSN(iam, "mydb")
	if strings.Contains(got, "password=") {
		t.Errorf("gcpDSN IAM auth should omit password, got %q", got)
	}
	if !strings.Contains(got, "user='sa@proj.iam'") {
		t.Errorf("gcpDSN IAM auth = %q, missing IAM user", got)
	}
}

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

func TestGCPConnSpec(t *testing.T) {
	cases := []struct {
		name    string
		in      *Config
		want    gcpSpec
		wantErr bool
	}{
		{"default", &Config{Host: "p:r:i"}, gcpSpec{IPType: "auto", UseDNS: false, IAMAuth: false}, false},
		{"private", &Config{Host: "p:r:i", GCPIPType: "private"}, gcpSpec{IPType: "private"}, false},
		{"psc", &Config{Host: "p:r:i", GCPIPType: "psc"}, gcpSpec{IPType: "psc"}, false},
		{"iam", &Config{Host: "p:r:i", GCPIAMAuth: true}, gcpSpec{IPType: "auto", IAMAuth: true}, false},
		{"dns flag", &Config{Host: "p:r:i", GCPDNS: true}, gcpSpec{IPType: "auto", UseDNS: true}, false},
		{"domain host", &Config{Host: "db.example.com"}, gcpSpec{IPType: "auto", UseDNS: true}, false},
		{"impersonate", &Config{Host: "p:r:i", GCPIAMImpersonateServiceAccount: "sa@p.iam"}, gcpSpec{IPType: "auto", Impersonate: "sa@p.iam"}, false},
		{"invalid", &Config{Host: "p:r:i", GCPIPType: "bogus"}, gcpSpec{}, true},
	}
	for _, c := range cases {
		got, err := gcpConnSpec(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got nil", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: gcpConnSpec = %+v, want %+v", c.name, got, c.want)
		}
	}
}
