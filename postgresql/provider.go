package postgresql

import (
	"fmt"

	"github.com/blang/semver"
	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

const (
	defaultProviderMaxOpenConnections = uint(4)
	defaultExpectedPostgreSQLVersion  = "9.0.0"
)

// Provider returns a terraform.ResourceProvider.
func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
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
				Type:     schema.TypeBool,
				Optional: true,
				Default:  true,
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
			"connect_timeout": {
				Type:         schema.TypeInt,
				Optional:     true,
				DefaultFunc:  schema.EnvDefaultFunc("PGCONNECT_TIMEOUT", 180),
				Description:  "Maximum wait for connection, in seconds. Zero or not specified means wait indefinitely.",
				ValidateFunc: validateConnTimeout,
			},
			"max_connections": {
				Type:         schema.TypeInt,
				Optional:     true,
				Default:      defaultProviderMaxOpenConnections,
				Description:  "Maximum number of connections to establish to the database. Zero means unlimited.",
				ValidateFunc: validateMaxConnections,
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
			"postgresql_database":  resourcePostgreSQLDatabase(),
			"postgresql_extension": resourcePostgreSQLExtension(),
			"postgresql_schema":    resourcePostgreSQLSchema(),
			"postgresql_role":      resourcePostgreSQLRole(),
		},

		ConfigureFunc: providerConfigure,
	}
}

func validateConnTimeout(v interface{}, key string) (warnings []string, errors []error) {
	value := v.(int)
	if value < 0 {
		errors = append(errors, fmt.Errorf("%s can not be less than 0", key))
	}
	return
}

func validateExpectedVersion(v interface{}, key string) (warnings []string, errors []error) {
	if _, err := semver.Parse(v.(string)); err != nil {
		errors = append(errors, fmt.Errorf("invalid version (%q): %v", v.(string), err))
	}
	return
}

func validateMaxConnections(v interface{}, key string) (warnings []string, errors []error) {
	value := v.(int)
	if value < 1 {
		errors = append(errors, fmt.Errorf("%s can not be less than 1", key))
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
	version, _ := semver.Parse(versionStr)

	config := Config{
		Host:              d.Get("host").(string),
		Port:              d.Get("port").(int),
		Database:          d.Get("database").(string),
		Username:          d.Get("username").(string),
		Password:          d.Get("password").(string),
		DatabaseUsername:  d.Get("database_username").(string),
		Superuser:         d.Get("superuser").(bool),
		SSLMode:           sslMode,
		ApplicationName:   tfAppName(),
		ConnectTimeoutSec: d.Get("connect_timeout").(int),
		MaxConns:          d.Get("max_connections").(int),
		ExpectedVersion:   version,
	}

	client, err := config.NewClient()
	if err != nil {
		return nil, errwrap.Wrapf("Error initializing PostgreSQL client: {{err}}", err)
	}

	return client, nil
}

func tfAppName() string {
	return fmt.Sprintf("Terraform v%s", terraform.VersionString())
}
