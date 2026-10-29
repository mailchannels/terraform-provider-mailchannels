package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccSubAccount(t *testing.T) {
	stub, cfg := testStub(t)
	var mark int
	conf := func(enabled bool) string {
		return cfg + fmt.Sprintf(`
resource "mailchannels_sub_account" "test" {
  handle       = "staging1"
  company_name = "Example Staging"
  enabled      = %t
}
`, enabled)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if _, ok := stub.Snapshot().SubAccounts["staging1"]; ok {
				return fmt.Errorf("sub-account staging1 still exists")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: conf(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_sub_account.test", "id", "staging1"),
					resource.TestCheckResourceAttr("mailchannels_sub_account.test", "enabled", "true"),
				),
			},
			{
				ResourceName:      "mailchannels_sub_account.test",
				ImportState:       true,
				ImportStateId:     "staging1",
				ImportStateVerify: true,
			},
			{
				// Suspending is an in-place update, not a replacement.
				PreConfig: markRequests(stub, &mark),
				Config:    conf(false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_sub_account.test", "enabled", "false"),
					checkRequests(stub, &mark, "POST", "/sub-account/staging1/suspend", 1),
					checkRequests(stub, &mark, "DELETE", "/sub-account/staging1", 0),
				),
			},
			{
				PreConfig: markRequests(stub, &mark),
				Config:    conf(true),
				Check:     checkRequests(stub, &mark, "POST", "/sub-account/staging1/activate", 1),
			},
			{
				// Renaming replaces the sub-account: the API cannot rename.
				PreConfig: markRequests(stub, &mark),
				Config:    strings.Replace(conf(true), "Example Staging", "Example QA", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_sub_account.test", "company_name", "Example QA"),
					checkRequests(stub, &mark, "DELETE", "/sub-account/staging1", 1),
					checkRequests(stub, &mark, "POST", "/sub-account", 1),
				),
			},
		},
	})
}

func TestAccSubAccount_generatedHandleSuspended(t *testing.T) {
	_, cfg := testStub(t)
	config := cfg + `
resource "mailchannels_sub_account" "test" {
  company_name = "Generated Handle"
  enabled      = false
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("mailchannels_sub_account.test", "handle", regexpMust(`^generated\d+$`)),
					resource.TestCheckResourceAttr("mailchannels_sub_account.test", "enabled", "false"),
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestAccSubAccountLimit(t *testing.T) {
	stub, cfg := testStub(t)
	var mark int
	conf := func(sends int) string {
		return cfg + fmt.Sprintf(`
resource "mailchannels_sub_account" "test" {
  handle       = "prod1"
  company_name = "Example Production"
}
resource "mailchannels_sub_account_limit" "test" {
  handle = mailchannels_sub_account.test.handle
  sends  = %d
}
`, sends)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: conf(1000),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_sub_account_limit.test", "id", "prod1"),
					resource.TestCheckResourceAttr("mailchannels_sub_account_limit.test", "sends", "1000"),
				),
			},
			{
				ResourceName:      "mailchannels_sub_account_limit.test",
				ImportState:       true,
				ImportStateId:     "prod1",
				ImportStateVerify: true,
			},
			{
				PreConfig: markRequests(stub, &mark),
				Config:    conf(0),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_sub_account_limit.test", "sends", "0"),
					checkRequests(stub, &mark, "PUT", "/sub-account/prod1/limit", 1),
					func(*terraform.State) error {
						if got := stub.Snapshot().SubAccounts["prod1"]; got != 0 {
							return fmt.Errorf("stub limit is %d, want 0", got)
						}
						return nil
					},
				),
			},
			{
				// Removing the limit resource restores the inherited limit.
				Config: cfg + `
resource "mailchannels_sub_account" "test" {
  handle       = "prod1"
  company_name = "Example Production"
}
`,
				Check: func(*terraform.State) error {
					if got := stub.Snapshot().SubAccounts["prod1"]; got != inheritedLimit {
						return fmt.Errorf("stub limit is %d, want %d", got, inheritedLimit)
					}
					return nil
				},
			},
		},
	})
}

func TestAccSubAccountLimit_invalid(t *testing.T) {
	_, cfg := testStub(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg + `
resource "mailchannels_sub_account_limit" "test" {
  handle = "prod1"
  sends  = -1
}
`,
			ExpectError: regexpMust("must be at least 0"),
		}},
	})
}
