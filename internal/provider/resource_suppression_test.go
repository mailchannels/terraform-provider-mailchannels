package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccSuppression(t *testing.T) {
	stub, cfg := testStub(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := stub.Snapshot().Suppressions; len(got) != 0 {
				return fmt.Errorf("suppressions left after destroy: %v", got)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: cfg + `
resource "mailchannels_suppression" "test" {
  recipient         = "bounce@example.test"
  suppression_types = ["transactional", "non-transactional"]
  notes             = "synthetic test entry"
}
resource "mailchannels_suppression" "defaults" {
  recipient = "defaults@example.test"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_suppression.test", "id", "bounce@example.test"),
					resource.TestCheckResourceAttr("mailchannels_suppression.test", "suppression_types.#", "2"),
					resource.TestCheckResourceAttr("mailchannels_suppression.test", "notes", "synthetic test entry"),
					resource.TestCheckResourceAttrSet("mailchannels_suppression.test", "created_at"),
					resource.TestCheckResourceAttr("mailchannels_suppression.defaults", "suppression_types.#", "1"),
					resource.TestCheckNoResourceAttr("mailchannels_suppression.defaults", "notes"),
				),
			},
			{
				ResourceName:      "mailchannels_suppression.test",
				ImportState:       true,
				ImportStateId:     "bounce@example.test",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccSuppression_invalidType(t *testing.T) {
	_, cfg := testStub(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg + `
resource "mailchannels_suppression" "test" {
  recipient         = "bounce@example.test"
  suppression_types = ["bogus"]
}
`,
				ExpectError: regexpMust(`value must be one of`),
			},
		},
	})
}
