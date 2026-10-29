package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDKIMKey(t *testing.T) {
	stub, cfg := testStub(t)
	config := cfg + `
resource "mailchannels_dkim_key" "test" {
  domain   = "example.test"
  selector = "mc1"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if got := stub.Snapshot().DKIMKeys["example.test/mc1"]; got != "retired" {
				return fmt.Errorf("after destroy the key is %q, want retired", got)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "id", "example.test/mc1"),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "algorithm", "rsa"),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "key_length", "2048"),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "status", "active"),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "dns_record_name", "mc1._domainkey.example.test"),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "dns_record_type", "TXT"),
					resource.TestMatchResourceAttr("mailchannels_dkim_key.test", "dns_record_value", regexpMust(`^v=DKIM1; k=rsa; p=[A-Za-z0-9+/=]{300,}$`)),
					resource.TestCheckResourceAttr("mailchannels_dkim_key.test", "dns_record_value_chunks.#", "2"),
					func(s *terraform.State) error {
						a := s.RootModule().Resources["mailchannels_dkim_key.test"].Primary.Attributes
						if a["dns_record_value_chunks.0"]+a["dns_record_value_chunks.1"] != a["dns_record_value"] || len(a["dns_record_value_chunks.0"]) != 255 {
							return fmt.Errorf("chunks do not reassemble dns_record_value")
						}
						if !strings.Contains(a["dns_record_value"], a["public_key"]) {
							return fmt.Errorf("dns_record_value does not contain public_key")
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "mailchannels_dkim_key.test",
				ImportState:       true,
				ImportStateId:     "example.test/mc1",
				ImportStateVerify: true,
			},
			{
				// Replacing a key under the same selector fails: the API keeps
				// retired keys, so the selector stays taken.
				Config: strings.Replace(config, `selector = "mc1"`, `selector = "mc1"
  key_length = 1024`, 1),
				ExpectError: regexpMust("409"),
			},
		},
	})
}

func TestAccDKIMKey_replaceWithNewSelector(t *testing.T) {
	stub, cfg := testStub(t)
	conf := func(selector string) string {
		return cfg + fmt.Sprintf(`
resource "mailchannels_dkim_key" "test" {
  domain     = "example.test"
  selector   = %q
  key_length = 1024
}
`, selector)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: conf("old")},
			{
				Config: conf("new"),
				Check: func(*terraform.State) error {
					keys := stub.Snapshot().DKIMKeys
					if keys["example.test/old"] != "retired" || keys["example.test/new"] != "active" {
						return fmt.Errorf("want old retired and new active, got %v", keys)
					}
					return nil
				},
			},
		},
	})
}

func TestAccDKIMKey_retiredOutsideTerraform(t *testing.T) {
	stub, cfg := testStub(t)
	config := cfg + `
resource "mailchannels_dkim_key" "test" {
  domain   = "example.test"
  selector = "mc1"
}
`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					stub.Reset()
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestChunk(t *testing.T) {
	cases := map[int][]int{0: {0}, 255: {255}, 256: {255, 1}, 600: {255, 255, 90}}
	for n, want := range cases {
		got := chunk(strings.Repeat("a", n), 255)
		if len(got) != len(want) {
			t.Fatalf("chunk(%d): got %d chunks, want %d", n, len(got), len(want))
		}
		for i := range got {
			if len(got[i]) != want[i] {
				t.Errorf("chunk(%d)[%d]: got %d, want %d", n, i, len(got[i]), want[i])
			}
		}
	}
}
