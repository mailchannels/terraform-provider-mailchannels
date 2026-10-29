package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccCheckDomainDataSource(t *testing.T) {
	_, cfg := testStub(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg + `
resource "mailchannels_dkim_key" "test" {
  domain     = "example.test"
  selector   = "mc1"
  key_length = 1024
}
data "mailchannels_check_domain" "test" {
  domain = mailchannels_dkim_key.test.domain
  dkim_settings = [{
    dkim_domain   = mailchannels_dkim_key.test.domain
    dkim_selector = mailchannels_dkim_key.test.selector
  }]
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.mailchannels_check_domain.test", "all_passed", "true"),
				resource.TestCheckResourceAttr("data.mailchannels_check_domain.test", "dkim.#", "1"),
				resource.TestCheckResourceAttr("data.mailchannels_check_domain.test", "dkim.0.selector", "mc1"),
				resource.TestCheckResourceAttr("data.mailchannels_check_domain.test", "spf_verdict", "passed"),
			),
		}},
	})
}
