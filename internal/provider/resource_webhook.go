package provider

import (
	"context"
	"slices"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

var (
	_ resource.Resource                = &webhookResource{}
	_ resource.ResourceWithConfigure   = &webhookResource{}
	_ resource.ResourceWithImportState = &webhookResource{}
)

// webhookMu serialises webhook changes in this provider process, because
// deleting one endpoint means deleting all of them and registering the
// rest again.
var webhookMu sync.Mutex

// NewWebhookResource returns the mailchannels_webhook resource.
func NewWebhookResource() resource.Resource { return &webhookResource{} }

type webhookResource struct{ client *client.Client }

type webhookModel struct {
	ID       types.String `tfsdk:"id"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A webhook endpoint that receives delivery events for the account.\n\n" +
			"The Email API can only delete all of an account's webhooks at once. To destroy one `mailchannels_webhook`, " +
			"the provider deletes every endpoint and registers the others again, so events can be missed for a moment " +
			"and an endpoint registered by another client at the same time can be lost.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The endpoint URL.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "HTTPS URL that receives the events. Changing it replaces the webhook.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhookMu.Lock()
	defer webhookMu.Unlock()
	if err := r.client.CreateWebhook(ctx, plan.Endpoint.ValueString()); err != nil {
		resp.Diagnostics.AddError("Creating webhook", err.Error()+
			"\n\nIf the endpoint is already registered, import it with: terraform import <address> "+plan.Endpoint.ValueString())
		return
	}
	plan.ID = plan.Endpoint
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoints, err := r.client.ListWebhooks(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading webhooks", err.Error())
		return
	}
	if !slices.Contains(endpoints, state.Endpoint.ValueString()) {
		resp.State.RemoveResource(ctx)
		return
	}
	state.ID = state.Endpoint
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: the endpoint requires replacement.
func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Webhooks cannot be updated", "Changing the endpoint replaces the webhook.")
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhookMu.Lock()
	defer webhookMu.Unlock()
	endpoints, err := r.client.ListWebhooks(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading webhooks", err.Error())
		return
	}
	if !slices.Contains(endpoints, state.Endpoint.ValueString()) {
		return
	}
	if err := r.client.DeleteAllWebhooks(ctx); err != nil {
		resp.Diagnostics.AddError("Deleting webhooks", err.Error())
		return
	}
	for _, e := range endpoints {
		if e == state.Endpoint.ValueString() {
			continue
		}
		if err := r.client.CreateWebhook(ctx, e); err != nil {
			resp.Diagnostics.AddError("Registering remaining webhook again",
				"Deleting a webhook deletes all of them; registering "+e+" again failed: "+err.Error())
		}
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("endpoint"), req.ID)...)
}
