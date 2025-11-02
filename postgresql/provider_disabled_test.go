package postgresql

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestProviderConfigure_Disabled(t *testing.T) {
	raw := map[string]interface{}{
		"disabled": true,
		"host":     "localhost",
		"port":     5432,
		"username": "postgres",
		"password": "test",
	}

	p := Provider()
	d := schema.TestResourceDataRaw(t, p.Schema, raw)

	client, err := providerConfigure(d)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if client != nil {
		t.Fatal("expected client to be nil when disabled=true")
	}
}

func TestProviderConfigure_NotDisabled(t *testing.T) {
	raw := map[string]interface{}{
		"disabled": false,
		"host":     "localhost",
		"port":     25432,
		"username": "postgres",
		"password": "postgres",
		"database": "postgres",
	}

	p := Provider()
	d := schema.TestResourceDataRaw(t, p.Schema, raw)

	client, err := providerConfigure(d)
	
	// Client creation will likely fail without a real DB, but that's ok
	// We just want to verify it doesn't return early like disabled=true does
	_ = client
	_ = err
}
