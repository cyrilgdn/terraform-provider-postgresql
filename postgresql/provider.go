package postgresql

import (
	"fmt"

	"github.com/blang/semver"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

const (
	defaultProviderMaxOpenConnections = 20
	defaultExpectedPostgreSQLVersion  = "9.0.0"
)

// Provider returns a terraform.ResourceProvider.
func Provider() *schema.Provider {
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
		},

		ResourcesMap: map[string]*schema.Resource{
			"postgresql_database":                  resourcePostgreSQLDatabase(),
			"postgresql_default_privileges":        resourcePostgreSQLDefaultPrivileges(),
			"postgresql_extension":                 resourcePostgreSQLExtension(),
			"postgresql_grant":                     resourcePostgreSQLGrant(),
			"postgresql_grant_role":                resourcePostgreSQLGrantRole(),
			"postgresql_replication_slot":          resourcePostgreSQLReplicationSlot(),
			"postgresql_physical_replication_slot": resourcePostgreSQLPhysicalReplicationSlot(),
			"postgresql_schema":                    resourcePostgreSQLSchema(),
			"postgresql_role":                      resourcePostgreSQLRole(),
		},

		DataSourcesMap: map[string]*schema.Resource{
			"postgresql_schemas": dataPostgreSQLDatabaseSchemas(),
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

	config := Config{
		Scheme:            d.Get("scheme").(string),
		Host:              d.Get("host").(string),
		Port:              d.Get("port").(int),
		Username:          d.Get("username").(string),
		Password:          d.Get("password").(string),
		DatabaseUsername:  d.Get("database_username").(string),
		Superuser:         d.Get("superuser").(bool),
		SSLMode:           sslMode,
		ApplicationName:   "Terraform provider",
		ConnectTimeoutSec: d.Get("connect_timeout").(int),
		MaxConns:          d.Get("max_connections").(int),
		ExpectedVersion:   version,
		SSLRootCertPath:   d.Get("sslrootcert").(string),
	}

	if value, ok := d.GetOk("clientcert"); ok {
		if spec, ok := value.([]interface{})[0].(map[string]interface{}); ok {
			config.SSLClientCert = &ClientCertificateConfig{
				CertificatePath: spec["cert"].(string),
				KeyPath:         spec["key"].(string),
			}
		}
	}

	client := config.NewClient(d.Get("database").(string))
	return client, nil
}
