package postgresql

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/blang/semver"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"golang.org/x/oauth2/google"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
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

			"aws_rds_iam_auth": {
				Type:     schema.TypeBool,
				Optional: true,
				Description: "Use rds_iam instead of password authentication " +
					"(see: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAMDBAuth.html)",
			},

			"aws_rds_iam_profile": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "AWS profile to use for IAM auth",
			},

			"aws_rds_iam_region": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "AWS region to use for IAM auth",
			},

			"azure_identity_auth": {
				Type:     schema.TypeBool,
				Optional: true,
				Description: "Use MS Azure identity OAuth token " +
					"(see: https://learn.microsoft.com/en-us/azure/postgresql/flexible-server/how-to-configure-sign-in-azure-ad-authentication)",
			},

			"azure_tenant_id": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "MS Azure tenant ID (see: https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/data-sources/client_config.html)",
			},

			"gcp_iam_impersonate_service_account": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Service account to impersonate when using GCP IAM authentication.",
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
						"sslinline": {
							Type:        schema.TypeBool,
							Description: "Must be set to true if you are inlining the cert/key instead of using a file path.",
							Optional:    true,
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
			"postgresql_publication":               resourcePostgreSQLPublication(),
			"postgresql_subscription":              resourcePostgreSQLSubscription(),
			"postgresql_physical_replication_slot": resourcePostgreSQLPhysicalReplicationSlot(),
			"postgresql_schema":                    resourcePostgreSQLSchema(),
			"postgresql_role":                      resourcePostgreSQLRole(),
			"postgresql_function":                  resourcePostgreSQLFunction(),
			"postgresql_server":                    resourcePostgreSQLServer(),
			"postgresql_user_mapping":              resourcePostgreSQLUserMapping(),
		},

		DataSourcesMap: map[string]*schema.Resource{
			"postgresql_schemas":   dataSourcePostgreSQLDatabaseSchemas(),
			"postgresql_tables":    dataSourcePostgreSQLDatabaseTables(),
			"postgresql_sequences": dataSourcePostgreSQLDatabaseSequences(),
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

func getRDSAuthToken(region string, profile string, username string, host string, port int) (string, error) {
	endpoint := fmt.Sprintf("%s:%d", host, port)

	ctx := context.Background()

	var awscfg aws.Config
	var err error

	if profile != "" {
		awscfg, err = awsConfig.LoadDefaultConfig(ctx, awsConfig.WithSharedConfigProfile(profile))
	} else if region != "" {
		awscfg, err = awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(region))
	} else {
		awscfg, err = awsConfig.LoadDefaultConfig(ctx)
	}
	if err != nil {
		return "", err
	}

	token, err := auth.BuildAuthToken(ctx, endpoint, awscfg.Region, username, awscfg.Credentials)

	return token, err
}

func createGoogleCredsFileIfNeeded() error {
	if _, err := google.FindDefaultCredentials(context.Background()); err == nil {
		return nil
	}

	rawGoogleCredentials := os.Getenv("GOOGLE_CREDENTIALS")
	if rawGoogleCredentials == "" {
		return nil
	}

	tmpFile, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create temporary file: %w", err)
	}
	defer tmpFile.Close()

	_, err = tmpFile.WriteString(rawGoogleCredentials)
	if err != nil {
		return fmt.Errorf("could not write in temporary file: %w", err)
	}

	return os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", tmpFile.Name())
}

func acquireAzureOauthToken(tenantId string) (string, error) {
	credential, err := azidentity.NewDefaultAzureCredential(
		&azidentity.DefaultAzureCredentialOptions{TenantID: tenantId})
	if err != nil {
		return "", err
	}
	token, err := credential.GetToken(context.Background(), policy.TokenRequestOptions{
		Scopes:   []string{"https://ossrdbms-aad.database.windows.net/.default"},
		TenantID: tenantId,
	})
	if err != nil {
		return "", err
	}
	return token.Token, nil
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

	host := d.Get("host").(string)
	port := d.Get("port").(int)
	username := d.Get("username").(string)

	var password string
	if d.Get("aws_rds_iam_auth").(bool) {
		profile := d.Get("aws_rds_iam_profile").(string)
		region := d.Get("aws_rds_iam_region").(string)
		var err error
		password, err = getRDSAuthToken(region, profile, username, host, port)
		if err != nil {
			return nil, err
		}
	} else if d.Get("azure_identity_auth").(bool) {
		tenantId := d.Get("azure_tenant_id").(string)
		if tenantId == "" {
			return nil, fmt.Errorf("postgresql: azure_identity_auth is enabled, azure_tenant_id must be provided also")
		}
		var err error
		password, err = acquireAzureOauthToken(tenantId)
		if err != nil {
			return nil, err
		}
	} else {
		password = d.Get("password").(string)
	}

	config := Config{
		Scheme:                          d.Get("scheme").(string),
		Host:                            host,
		Port:                            port,
		Username:                        username,
		Password:                        password,
		DatabaseUsername:                d.Get("database_username").(string),
		Superuser:                       d.Get("superuser").(bool),
		SSLMode:                         sslMode,
		ApplicationName:                 "Terraform provider",
		ConnectTimeoutSec:               d.Get("connect_timeout").(int),
		MaxConns:                        d.Get("max_connections").(int),
		ExpectedVersion:                 version,
		SSLRootCertPath:                 d.Get("sslrootcert").(string),
		GCPIAMImpersonateServiceAccount: d.Get("gcp_iam_impersonate_service_account").(string),
	}

	if value, ok := d.GetOk("clientcert"); ok {
		if spec, ok := value.([]interface{})[0].(map[string]interface{}); ok {
			config.SSLClientCert = &ClientCertificateConfig{
				CertificatePath: spec["cert"].(string),
				KeyPath:         spec["key"].(string),
				SSLInline:       spec["sslinline"].(bool),
			}
		}
	}

	if config.Scheme == "gcppostgres" {
		if err := createGoogleCredsFileIfNeeded(); err != nil {
			return nil, err
		}
	}

	client := config.NewClient(d.Get("database").(string))
	return client, nil
}
