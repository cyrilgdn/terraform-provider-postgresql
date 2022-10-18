package postgresql

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourcePostgrePassword() *schema.Resource {
	return &schema.Resource{
		Read: dataSourcePostgrePasswordRead,
		Schema: map[string]*schema.Schema{
			"password": {
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func dataSourcePostgrePasswordRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*Client)
	d.SetId("password")
	d.Set("password", client.config.Password)
	return nil
}
