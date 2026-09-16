package postgresql

import (
	"context"
	"os"
	"path/filepath"
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

// Detaches the test from any Application Default Credentials present on the host
// (gcloud config dir) so createGoogleCredsFileIfNeeded only sees what the test sets.
func isolateGoogleCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv(googleCredentialsEnvVar, "")
	t.Setenv("GOOGLE_CREDENTIALS", "")
}

func TestCreateGoogleCredsFile_explicitPathWinsOverInlineCredentials(t *testing.T) {
	isolateGoogleCredentials(t)
	t.Setenv("GOOGLE_CREDENTIALS", `{"type":"service_account","project_id":"inline-project"}`)

	if err := createGoogleCredsFileIfNeeded("/etc/gcp/sa-key.json"); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(googleCredentialsEnvVar); got != "/etc/gcp/sa-key.json" {
		t.Fatalf("%s = %q, want /etc/gcp/sa-key.json", googleCredentialsEnvVar, got)
	}
}

func TestCreateGoogleCredsFile_inlineCredentialsWrittenToTempFile(t *testing.T) {
	isolateGoogleCredentials(t)
	raw := `{"type":"service_account","project_id":"inline-project"}`
	t.Setenv("GOOGLE_CREDENTIALS", raw)

	if err := createGoogleCredsFileIfNeeded(""); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv(googleCredentialsEnvVar)
	if path == "" {
		t.Fatalf("%s not set", googleCredentialsEnvVar)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != raw {
		t.Fatalf("temp file content = %q, want %q", content, raw)
	}
}

func TestCreateGoogleCredsFile_existingADCFileLeftUntouched(t *testing.T) {
	isolateGoogleCredentials(t)
	adcPath := filepath.Join(t.TempDir(), "adc.json")
	adc := `{"type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`
	if err := os.WriteFile(adcPath, []byte(adc), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(googleCredentialsEnvVar, adcPath)
	t.Setenv("GOOGLE_CREDENTIALS", `{"type":"service_account","project_id":"inline-project"}`)

	if err := createGoogleCredsFileIfNeeded(""); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(googleCredentialsEnvVar); got != adcPath {
		t.Fatalf("%s = %q, want %q (ADC must win over GOOGLE_CREDENTIALS)", googleCredentialsEnvVar, got, adcPath)
	}
}

func TestCreateGoogleCredsFile_noCredentialsAnywhereIsNotAnError(t *testing.T) {
	isolateGoogleCredentials(t)

	if err := createGoogleCredsFileIfNeeded(""); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(googleCredentialsEnvVar); got != "" {
		t.Fatalf("%s = %q, want empty", googleCredentialsEnvVar, got)
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
