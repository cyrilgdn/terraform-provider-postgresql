package postgresql

import (
	"fmt"

	"github.com/blang/semver"
	"github.com/hashicorp/go-azure-helpers/authentication"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

const (
	defaultProviderMaxOpenConnections = 20
	defaultExpectedPostgreSQLVersion  = "9.0.0"
)

// Provider returns a terraform.ResourceProvider.
func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"scheme": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "postgres",
				ValidateFunc: validation.StringInSlice([]string{
					"postgres",
					"awspostgres",
					"gcppostgres",
				}, false),
			},
			"host": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGHOST", nil),
				Description: "Name of PostgreSQL server address to connect to",
			},
			"port": {
				Type:        schema.TypeInt,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGPORT", 5432),
				Description: "The PostgreSQL port number to connect to at the server host, or socket file name extension for Unix-domain connections",
			},
			"database": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The name of the database to connect to in order to conenct to (defaults to `postgres`).",
				DefaultFunc: schema.EnvDefaultFunc("PGDATABASE", "postgres"),
			},
			"username": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGUSER", "postgres"),
				Description: "PostgreSQL user name to connect as",
			},
			"password": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGPASSWORD", nil),
				Description: "Password to be used if the PostgreSQL server demands password authentication",
				Sensitive:   true,
			},
			// Conection username can be different than database username with user name mapas (e.g.: in Azure)
			// See https://www.postgresql.org/docs/current/auth-username-maps.html
			"database_username": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Database username associated to the connected user (for user name maps)",
			},

			"superuser": {
				Type:        schema.TypeBool,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGSUPERUSER", true),
				Description: "Specify if the user to connect as is a Postgres superuser or not." +
					"If not, some feature might be disabled (e.g.: Refreshing state password from Postgres)",
			},

			"sslmode": {
				Type:        schema.TypeString,
				Optional:    true,
				DefaultFunc: schema.EnvDefaultFunc("PGSSLMODE", nil),
				Description: "This option determines whether or with what priority a secure SSL TCP/IP connection will be negotiated with the PostgreSQL server",
			},
			"ssl_mode": {
				Type:       schema.TypeString,
				Optional:   true,
				Deprecated: "Rename PostgreSQL provider `ssl_mode` attribute to `sslmode`",
			},
			"clientcert": {
				Type:        schema.TypeList,
				Optional:    true,
				Description: "SSL client certificate if required by the database.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"cert": {
							Type:        schema.TypeString,
							Description: "The SSL client certificate file path. The file must contain PEM encoded data.",
							Required:    true,
						},
						"key": {
							Type:        schema.TypeString,
							Description: "The SSL client certificate private key file path. The file must contain PEM encoded data.",
							Required:    true,
						},
					},
				},
				MaxItems: 1,
			},
			"sslrootcert": {
				Type:        schema.TypeString,
				Description: "The SSL server root certificate file path. The file must contain PEM encoded data.",
				Optional:    true,
			},

			"connect_timeout": {
				Type:         schema.TypeInt,
				Optional:     true,
				DefaultFunc:  schema.EnvDefaultFunc("PGCONNECT_TIMEOUT", 180),
				Description:  "Maximum wait for connection, in seconds. Zero or not specified means wait indefinitely.",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			"max_connections": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      defaultProviderMaxOpenConnections,
				Description:  "Maximum number of connections to establish to the database. Zero means unlimited.",
				ValidateFunc: validation.IntAtLeast(-1),
			},
			"expected_version": {
				Type:         schema.TypeString,
				Optional:     true,
				Default:      defaultExpectedPostgreSQLVersion,
				Description:  "Specify the expected version of PostgreSQL.",
				ValidateFunc: validateExpectedVersion,
			},
			"azure_ad_authentication": {
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    1,
				Description: "Connection details for connecting using Azure Active Directory.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"client_id": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_CLIENT_ID", ""),
							Description: "The Client ID which should be used for service principal authentication.",
						},

						"tenant_id": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_TENANT_ID", ""),
							Description: "The Tenant ID which should be used. Works with all authentication methods except MSI.",
						},

						"metadata_host": {
							Type:        schema.TypeString,
							Required:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_METADATA_HOSTNAME", ""),
							Description: "The Hostname which should be used for the Azure Metadata Service.",
						},

						"environment": {
							Type:        schema.TypeString,
							Required:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_ENVIRONMENT", "public"),
							Description: "The Cloud Environment which should be used. Possible values are `public`, `usgovernment`, `german`, and `china`. Defaults to `public`.",
						},

						// Client Certificate specific fields
						"client_certificate_password": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_CLIENT_CERTIFICATE_PASSWORD", ""),
						},

						"client_certificate_path": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_CLIENT_CERTIFICATE_PATH", ""),
							Description: "The path to the Client Certificate associated with the Service Principal for use when authenticating as a Service Principal using a Client Certificate.",
						},

						// Client Secret specific fields
						"client_secret": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_CLIENT_SECRET", ""),
							Description: "The password to decrypt the Client Certificate. For use when authenticating as a Service Principal using a Client Certificate",
						},

						// Managed Service Identity specific fields
						"use_msi": {
							Type:        schema.TypeBool,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_USE_MSI", false),
							Description: "Allow Managed Service Identity to be used for Authentication.",
						},

						"msi_endpoint": {
							Type:        schema.TypeString,
							Optional:    true,
							DefaultFunc: schema.EnvDefaultFunc("ARM_MSI_ENDPOINT", ""),
							Description: "The path to a custom endpoint for Managed Service Identity - in most circumstances this should be detected automatically. ",
						},
					},
				},
			},
		},

		ResourcesMap: map[string]*schema.Resource{
			"postgresql_database":           resourcePostgreSQLDatabase(),
			"postgresql_default_privileges": resourcePostgreSQLDefaultPrivileges(),
			"postgresql_extension":          resourcePostgreSQLExtension(),
			"postgresql_grant":              resourcePostgreSQLGrant(),
			"postgresql_grant_role":         resourcePostgreSQLGrantRole(),
			"postgresql_schema":             resourcePostgreSQLSchema(),
			"postgresql_role":               resourcePostgreSQLRole(),
		},

		ConfigureFunc: providerConfigure,
	}
}

func validateExpectedVersion(v interface{}, key string) (warnings []string, errors []error) {
	if _, err := semver.ParseTolerant(v.(string)); err != nil {
		errors = append(errors, fmt.Errorf("invalid version (%q): %w", v.(string), err))
	}
	return
}

// Microsoft’s Terraform Partner ID is this specific GUID
const terraformPartnerId = "222c6c49-1b0a-5959-a213-6608f9eb8820"

func providerConfigure(d *schema.ResourceData) (interface{}, error) {
	var sslMode string
	if sslModeRaw, ok := d.GetOk("sslmode"); ok {
		sslMode = sslModeRaw.(string)
	} else {
		sslModeDeprecated := d.Get("ssl_mode").(string)
		if sslModeDeprecated != "" {
			sslMode = sslModeDeprecated
		}
	}
	versionStr := d.Get("expected_version").(string)
	version, _ := semver.ParseTolerant(versionStr)

	azureADAuthenticationConfig, err := expandAzureADAuthentication(d.Get("azure_ad_authentication").([]interface{}))
	if err != nil {
		return nil, err
	}

	config := Config{
		Scheme:                      d.Get("scheme").(string),
		Host:                        d.Get("host").(string),
		Port:                        d.Get("port").(int),
		Username:                    d.Get("username").(string),
		Password:                    d.Get("password").(string),
		DatabaseUsername:            d.Get("database_username").(string),
		Superuser:                   d.Get("superuser").(bool),
		SSLMode:                     sslMode,
		ApplicationName:             "Terraform provider",
		ConnectTimeoutSec:           d.Get("connect_timeout").(int),
		MaxConns:                    d.Get("max_connections").(int),
		ExpectedVersion:             version,
		SSLRootCertPath:             d.Get("sslrootcert").(string),
		AzureADAuthenticationConfig: azureADAuthenticationConfig,
	}

	if value, ok := d.GetOk("clientcert"); ok {
		if spec, ok := value.([]interface{})[0].(map[string]interface{}); ok {
			config.SSLClientCert = &ClientCertificateConfig{
				CertificatePath: spec["cert"].(string),
				KeyPath:         spec["key"].(string),
			}
		}
	}

	client, err := config.NewClient(d.Get("database").(string))
	if err != nil {
		return nil, fmt.Errorf("could not create client: %w", err)
	}
	return client, nil
}

func expandAzureADAuthentication(in []interface{}) (*authentication.Config, error) {
	for _, raw := range in {
		d := raw.(map[string]interface{})
		builder := &authentication.Builder{
			ClientID:           d["client_id"].(string),
			ClientSecret:       d["client_secret"].(string),
			TenantID:           d["tenant_id"].(string),
			MetadataHost:       d["metadata_host"].(string),
			Environment:        d["environment"].(string),
			MsiEndpoint:        d["msi_endpoint"].(string),
			ClientCertPassword: d["client_certificate_password"].(string),
			ClientCertPath:     d["client_certificate_path"].(string),

			// Feature Toggles
			SupportsClientCertAuth:         true,
			SupportsClientSecretAuth:       true,
			SupportsManagedServiceIdentity: d["use_msi"].(bool),
			SupportsAzureCliToken:          true,
			TenantOnly:                     true,
		}
		config, err := builder.Build()
		if err != nil {
			return nil, fmt.Errorf("error building AzureAD Client: %w", err)
		}
		return config, nil
	}

	return nil, nil
}
