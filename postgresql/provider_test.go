package postgresql

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

var testAccProviders map[string]*schema.Provider
var testAccProvider *schema.Provider

func init() {
	testAccProvider = Provider()
	testAccProviders = map[string]*schema.Provider{
		"postgresql": testAccProvider,
	}
}

func TestProvider(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ = Provider()
}

func TestProvider_oidcSchema(t *testing.T) {
	s := Provider().Schema
	for _, key := range []string{"use_oidc", "azure_client_id", "oidc_token_file_path", "oidc_token"} {
		if _, ok := s[key]; !ok {
			t.Errorf("expected provider schema to contain %q", key)
		}
	}
	if !s["oidc_token"].Sensitive {
		t.Error("oidc_token must be sensitive")
	}
}

func TestProvider_oidcEnvDefaults(t *testing.T) {
	t.Setenv("ARM_USE_OIDC", "true")
	t.Setenv("ARM_CLIENT_ID", "client-from-env")
	t.Setenv("ARM_OIDC_TOKEN_FILE_PATH", "/tmp/token-from-env")

	d := schema.TestResourceDataRaw(t, Provider().Schema, map[string]interface{}{})
	if !d.Get("use_oidc").(bool) {
		t.Error("use_oidc should default from ARM_USE_OIDC")
	}
	if got := d.Get("azure_client_id").(string); got != "client-from-env" {
		t.Errorf("azure_client_id = %q, want client-from-env", got)
	}
	if got := d.Get("oidc_token_file_path").(string); got != "/tmp/token-from-env" {
		t.Errorf("oidc_token_file_path = %q, want /tmp/token-from-env", got)
	}
}

func TestOIDCAssertionFunc(t *testing.T) {
	ctx := context.Background()

	t.Run("reads and trims file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("jwt-1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := oidcAssertionFunc(path, "")(ctx)
		if err != nil || got != "jwt-1" {
			t.Fatalf("got (%q, %v), want (jwt-1, nil)", got, err)
		}
	})

	t.Run("re-reads file on every call", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		fn := oidcAssertionFunc(path, "")
		for _, want := range []string{"jwt-1", "jwt-2"} {
			if err := os.WriteFile(path, []byte(want), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := fn(ctx)
			if err != nil || got != want {
				t.Fatalf("got (%q, %v), want (%s, nil)", got, err, want)
			}
		}
	})

	t.Run("file takes precedence over inline token", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("from-file"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := oidcAssertionFunc(path, "inline")(ctx)
		if err != nil || got != "from-file" {
			t.Fatalf("got (%q, %v), want (from-file, nil)", got, err)
		}
	})

	t.Run("inline token", func(t *testing.T) {
		got, err := oidcAssertionFunc("", "inline")(ctx)
		if err != nil || got != "inline" {
			t.Fatalf("got (%q, %v), want (inline, nil)", got, err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := oidcAssertionFunc(filepath.Join(t.TempDir(), "nope"), "")(ctx); err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("  \n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := oidcAssertionFunc(path, "")(ctx); err == nil {
			t.Fatal("expected error for empty file")
		}
	})

	t.Run("no token source", func(t *testing.T) {
		if _, err := oidcAssertionFunc("", "")(ctx); err == nil {
			t.Fatal("expected error when no token source is set")
		}
	})
}

func TestProviderConfigure_oidcValidation(t *testing.T) {
	tests := []struct {
		name    string
		raw     map[string]interface{}
		wantErr string
	}{
		{
			name:    "missing tenant id",
			raw:     map[string]interface{}{"use_oidc": true, "azure_client_id": "c", "oidc_token": "t"},
			wantErr: "azure_tenant_id must be provided",
		},
		{
			name:    "missing client id",
			raw:     map[string]interface{}{"use_oidc": true, "azure_tenant_id": "t", "oidc_token": "t"},
			wantErr: "azure_client_id must be provided",
		},
		{
			name:    "missing token source",
			raw:     map[string]interface{}{"use_oidc": true, "azure_tenant_id": "t", "azure_client_id": "c"},
			wantErr: "neither oidc_token nor oidc_token_file_path",
		},
		{
			name: "unreadable token file",
			raw: map[string]interface{}{
				"use_oidc": true, "azure_tenant_id": "t", "azure_client_id": "c",
				"oidc_token_file_path": "/nonexistent/spacelift.oidc",
			},
			wantErr: "reading OIDC token file",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := schema.TestResourceDataRaw(t, Provider().Schema, tc.raw)
			_, err := providerConfigure(d)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got error %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func testAccPreCheck(t *testing.T) {
	var host string
	if host = os.Getenv("PGHOST"); host == "" {
		t.Fatal("PGHOST must be set for acceptance tests")
	}
	if v := os.Getenv("PGUSER"); v == "" {
		t.Fatal("PGUSER must be set for acceptance tests")
	}

	err := testAccProvider.Configure(context.Background(), terraform.NewResourceConfigRaw(nil))
	if err != nil {
		t.Fatal(err)
	}
}
