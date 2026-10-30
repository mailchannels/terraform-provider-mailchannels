// Package provider implements the MailChannels Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

// Environment variables read when the provider block omits an argument.
const (
	EnvAPIKey = "MAILCHANNELS_API_KEY"
	EnvAPIURL = "MAILCHANNELS_API_URL"
)

var _ provider.Provider = &mailchannelsProvider{}

type mailchannelsProvider struct {
	version string
}

type providerModel struct {
	APIKey types.String `tfsdk:"api_key"`
	APIURL types.String `tfsdk:"api_url"`
}

// New returns a provider factory for version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &mailchannelsProvider{version: version}
	}
}

func (p *mailchannelsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "mailchannels"
	resp.Version = p.version
}

func (p *mailchannelsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages [MailChannels Email API](https://docs.mailchannels.net/email-api) resources: " +
			"DKIM keys, webhooks, sub-accounts, sub-account limits and suppression list entries.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "Email API key. Defaults to the `" + EnvAPIKey + "` environment variable. " +
					"Sub-account resources need a parent account's key.",
				Optional:  true,
				Sensitive: true,
			},
			"api_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the Email API. Defaults to the `" + EnvAPIURL +
					"` environment variable, or `" + client.DefaultBaseURL + "`.",
				Optional: true,
			},
		},
	}
}

func (p *mailchannelsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Unknown API key",
			"The api_key is not known until apply. Set it to a value known at plan time, or use the "+EnvAPIKey+" environment variable.")
	}
	if cfg.APIURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Unknown API URL",
			"The api_url is not known until apply. Set it to a value known at plan time, or use the "+EnvAPIURL+" environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := os.Getenv(EnvAPIKey)
	if !cfg.APIKey.IsNull() {
		apiKey = cfg.APIKey.ValueString()
	}
	apiURL := os.Getenv(EnvAPIURL)
	if !cfg.APIURL.IsNull() {
		apiURL = cfg.APIURL.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing API key",
			"Set api_key in the provider block or the "+EnvAPIKey+" environment variable.")
		return
	}

	c := client.New(apiURL, apiKey, "terraform-provider-mailchannels/"+p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *mailchannelsProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDKIMKeyResource,
		NewWebhookResource,
		NewSubAccountResource,
		NewSubAccountLimitResource,
		NewSuppressionResource,
	}
}

func (p *mailchannelsProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCheckDomainDataSource,
	}
}
