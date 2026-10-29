package provider

import (
	"fmt"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccWebhook(t *testing.T) {
	stub, cfg := testStub(t)
	two := cfg + `
resource "mailchannels_webhook" "a" {
  endpoint = "https://hooks.example.test/a"
}
resource "mailchannels_webhook" "b" {
  endpoint = "https://hooks.example.test/b"
}
`
	one := cfg + `
resource "mailchannels_webhook" "b" {
  endpoint = "https://hooks.example.test/b"
}
`
	webhooks := func(want ...string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got := stub.Snapshot().Webhooks
			slices.Sort(got)
			if !slices.Equal(got, want) {
				return fmt.Errorf("stub webhooks: got %v, want %v", got, want)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             webhooks(),
		Steps: []resource.TestStep{
			{
				Config: two,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_webhook.a", "id", "https://hooks.example.test/a"),
					webhooks("https://hooks.example.test/a", "https://hooks.example.test/b"),
				),
			},
			{
				ResourceName:      "mailchannels_webhook.a",
				ImportState:       true,
				ImportStateId:     "https://hooks.example.test/a",
				ImportStateVerify: true,
			},
			{
				// Destroying one webhook keeps the other registered.
				Config: one,
				Check:  webhooks("https://hooks.example.test/b"),
			},
		},
	})
}

func TestAccWebhook_import(t *testing.T) {
	stub, cfg := testStub(t)
	config := cfg + `
resource "mailchannels_webhook" "existing" {
  endpoint = "https://hooks.example.test/existing"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					if err := testClient(stub).CreateWebhook(t.Context(), "https://hooks.example.test/existing"); err != nil {
						t.Fatal(err)
					}
				},
				Config:             config,
				ResourceName:       "mailchannels_webhook.existing",
				ImportState:        true,
				ImportStateId:      "https://hooks.example.test/existing",
				ImportStatePersist: true,
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
