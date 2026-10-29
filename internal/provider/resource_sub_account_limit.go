package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

// inheritedLimit is the limit the API reports for a sub-account without its own.
const inheritedLimit = -1

var (
	_ resource.Resource                = &subAccountLimitResource{}
	_ resource.ResourceWithConfigure   = &subAccountLimitResource{}
	_ resource.ResourceWithImportState = &subAccountLimitResource{}
)

// NewSubAccountLimitResource returns the mailchannels_sub_account_limit resource.
func NewSubAccountLimitResource() resource.Resource { return &subAccountLimitResource{} }

type subAccountLimitResource struct{ client *client.Client }

type subAccountLimitModel struct {
	ID     types.String `tfsdk:"id"`
	Handle types.String `tfsdk:"handle"`
	Sends  types.Int64  `tfsdk:"sends"`
}

func (r *subAccountLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sub_account_limit"
}

func (r *subAccountLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The monthly send limit of a sub-account. Destroying it removes the limit, so the " +
			"sub-account shares the parent account's remaining capacity again.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The sub-account handle.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"handle": schema.StringAttribute{
				MarkdownDescription: "Handle of the sub-account.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"sends": schema.Int64Attribute{
				MarkdownDescription: "Messages the sub-account may send per month, 0 or more.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(0)},
			},
		},
	}
}

func (r *subAccountLimitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *subAccountLimitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan subAccountLimitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.set(ctx, &plan, &resp.State)...)
}

func (r *subAccountLimitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan subAccountLimitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.set(ctx, &plan, &resp.State)...)
}

func (r *subAccountLimitResource) set(ctx context.Context, plan *subAccountLimitModel, state *tfsdk.State) (diags diag.Diagnostics) {
	if err := r.client.SetSubAccountLimit(ctx, plan.Handle.ValueString(), plan.Sends.ValueInt64()); err != nil {
		diags.AddError("Setting sub-account limit", err.Error())
		return diags
	}
	plan.ID = plan.Handle
	return state.Set(ctx, plan)
}

func (r *subAccountLimitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state subAccountLimitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	sends, err := r.client.GetSubAccountLimit(ctx, state.Handle.ValueString())
	if client.IsNotFound(err) || (err == nil && sends == inheritedLimit) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading sub-account limit", err.Error())
		return
	}
	state.ID = state.Handle
	state.Sends = types.Int64Value(sends)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *subAccountLimitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state subAccountLimitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteSubAccountLimit(ctx, state.Handle.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting sub-account limit", err.Error())
	}
}

func (r *subAccountLimitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("handle"), req.ID)...)
}
