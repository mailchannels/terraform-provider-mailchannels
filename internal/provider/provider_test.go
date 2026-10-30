package provider

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
	"github.com/mailchannels/terraform-provider-mailchannels/internal/stubapi"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"mailchannels": providerserver.NewProtocol6WithError(New("test")()),
}

// testStub starts an Email API stub for one test and returns it with a
// provider block that points at it with a synthetic key.
func testStub(t *testing.T) (*stubapi.Server, string) {
	t.Helper()
	stub, err := stubapi.New()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	stubURLs[stub] = srv.URL + stubapi.BasePath
	t.Cleanup(func() {
		for _, e := range stub.ValidationErrors() {
			t.Errorf("OpenAPI validation: %s", e)
		}
	})
	return stub, fmt.Sprintf(`
provider "mailchannels" {
  api_url = %q
  api_key = "synthetic-test-key"
}
`, srv.URL+stubapi.BasePath)
}

var stubURLs = map[*stubapi.Server]string{}

// testClient returns an API client for the stub, to set up state that
// exists before Terraform runs.
func testClient(stub *stubapi.Server) *client.Client {
	return client.New(stubURLs[stub], "synthetic-test-key", "test")
}

// countRequests counts logged requests with method and path; it is used to
// assert that an apply sent exactly the expected calls.
func countRequests(stub *stubapi.Server, method, path string) int {
	n := 0
	for _, r := range stub.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// checkRequests asserts the number of method+path requests since mark.
func checkRequests(stub *stubapi.Server, mark *int, method, path string, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got := 0
		for _, r := range stub.Requests()[*mark:] {
			if r.Method == method && r.Path == path {
				got++
			}
		}
		if got != want {
			return fmt.Errorf("got %d %s %s requests, want %d", got, method, path, want)
		}
		return nil
	}
}

func markRequests(stub *stubapi.Server, mark *int) func() {
	return func() { *mark = len(stub.Requests()) }
}

func TestAccProvider_missingAPIKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "mailchannels_check_domain" "x" { domain = "example.test" }`,
			ExpectError: regexpMust("Missing API key"),
		}},
	})
}

func TestAccProvider_apiKeyFromEnvironment(t *testing.T) {
	stub, cfg := testStub(t)
	// Keep api_url but take the key from the environment.
	cfg = strings.Replace(cfg, `api_key = "synthetic-test-key"`, "", 1)
	t.Setenv(EnvAPIKey, "synthetic-env-key")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: cfg + `data "mailchannels_check_domain" "x" { domain = "example.test" }`,
			Check: func(*terraform.State) error {
				if countRequests(stub, "POST", "/check-domain") == 0 {
					return fmt.Errorf("no check-domain request reached the stub")
				}
				return nil
			},
		}},
	})
}

func TestProviderSchema_apiKeySensitive(t *testing.T) {
	srv, err := testAccProtoV6ProviderFactories["mailchannels"]()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range resp.Provider.Block.Attributes {
		if a.Name == "api_key" && !a.Sensitive {
			t.Fatal("api_key must be sensitive")
		}
	}
}
