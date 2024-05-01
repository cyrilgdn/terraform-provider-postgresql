package postgresql

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

const (
	viewNameAttr        = "name"
	viewSchemaAttr      = "schema"
	viewRecursiveAttr   = "recursive"
	viewColumnNamesAttr = "column_names"
	viewBodyAttr        = "body"
	viewCheckOptionAttr = "check_option"
)

func resourcePostgreSQLView() *schema.Resource {
	return &schema.Resource{
		Create: PGResourceFunc(resourcePostgreSQLViewCreate),
		Read:   PGResourceFunc(resourcePostgreSQLViewCreate),
		Update: PGResourceFunc(resourcePostgreSQLViewCreate),
		Delete: PGResourceFunc(resourcePostgreSQLViewCreate),
		Exists: PGResourceExistsFunc(resourcePostgreSQLFunctionExists),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			viewSchemaAttr: {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
				Description: "Schema where the view is located. If not specified, the provider default is used.",

				DiffSuppressFunc: defaultDiffSuppressFunc,
			},
			viewNameAttr: {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of the view.",
			},
			viewRecursiveAttr: {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "If the view is a recursive view.",
			},
			viewColumnNamesAttr: {
				Type:        schema.TypeList,
				Optional:    true,
				Elem:        &schema.Schema{Type: schema.TypeString},
				Description: "The optional list of names to be used for columns of the view. If not given, the column names are deduced from the query.",
			},
			viewBodyAttr: {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Body of the view.",

				DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
					return normalizeFunctionBody(new) == old
				},
				StateFunc: func(val interface{}) string {
					return normalizeFunctionBody(val.(string))
				},
			},
			viewCheckOptionAttr: {
				Type:             schema.TypeString,
				Optional:         true,
				DiffSuppressFunc: defaultDiffSuppressFunc,
				ValidateFunc:     validation.StringInSlice([]string{"CASCADED", "LOCAL"}, true),
				Description:      "Controls the behavior of automatically updatable views. One of: CASCADED, LOCAL",
			},
		},
	}
}

func resourcePostgreSQLViewCreate(db *DBConnection, d *schema.ResourceData) error {
	if !db.featureSupported(featureFunction) {
		return fmt.Errorf(
			"postgresql_view resource is not supported for this Postgres version (%s)",
			db.version,
		)
	}

	if err := createView(db, d, false); err != nil {
		return err
	}

	return resourcePostgreSQLViewReadImpl(db, d)
}

func resourcePostgreSQLViewReadImpl(db *DBConnection, d *schema.ResourceData) error {
	return nil
}

func createView(db *DBConnection, d *schema.ResourceData, replace bool) error {
	return nil
}
