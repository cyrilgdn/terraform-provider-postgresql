package postgresql

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

var testProvidersLock = sync.Mutex{}
var testProviders = make(map[string]map[string]terraform.ResourceProvider)

func getTestProvider(t *testing.T) *schema.Provider {
	return getTestProvidersForTest(t)["postgresql"].(*schema.Provider)
}

func getTestProvidersForTest(t *testing.T) map[string]terraform.ResourceProvider {
	testProvidersLock.Lock()
	defer testProvidersLock.Unlock()
	providers, found := testProviders[t.Name()]
	if !found {
		resourceProvider := Provider(context.TODO())
		providers := map[string]terraform.ResourceProvider{
			"postgresql": resourceProvider.(*schema.Provider),
		}
		testProviders[t.Name()] = providers
		t.Cleanup(func() {
			err := resourceProvider.Stop()
			if err != nil {
				t.Errorf("failed to stop provider %v", err)
			}
		})
		return providers
	}
	return providers
}

func TestProvider(t *testing.T) {
	if err := getTestProvider(t).InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestProvider_impl(t *testing.T) {
	var _ = getTestProvider(t)
}

func testAccPreCheck(t *testing.T) {
	var host string
	if host = os.Getenv("PGHOST"); host == "" {
		t.Fatal("PGHOST must be set for acceptance tests")
	}
	if v := os.Getenv("PGUSER"); v == "" {
		t.Fatal("PGUSER must be set for acceptance tests")
	}

	err := getTestProvider(t).Configure(terraform.NewResourceConfigRaw(nil))
	if err != nil {
		t.Fatal(err)
	}
}
