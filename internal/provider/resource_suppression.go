package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

var (
	_ resource.Resource                = &suppressionResource{}
	_ resource.ResourceWithConfigure   = &suppressionResource{}
	_ resource.ResourceWithImportState = &suppressionResource{}
)

// NewSuppressionResource returns the mailchannels_suppression resource.
func NewSuppressionResource() resource.Resource { return &suppressionResource{} }

type suppressionResource struct{ client *client.Client }

type suppressionModel struct {
	ID               types.String `tfsdk:"id"`
	Recipient        types.String `tfsdk:"recipient"`
	SuppressionTypes types.Set    `tfsdk:"suppression_types"`
	Notes            types.String `tfsdk:"notes"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *suppressionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_suppression"
}

func (r *suppressionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A suppression list entry with source `api`: MailChannels does not deliver the listed " +
			"types of message to the recipient. Entries that MailChannels adds itself (hard bounces, complaints and " +
			"unsubscribes) are not managed. The Email API cannot change an entry, so changing any argument replaces it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The recipient.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"recipient": schema.StringAttribute{
				MarkdownDescription: "Email address to suppress.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"suppression_types": schema.SetAttribute{
				MarkdownDescription: "Types of message to suppress: `transactional`, `non-transactional` or both. " +
					"Defaults to what the Email API chooses when omitted.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf("transactional", "non-transactional")),
				},
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown(), setplanmodifier.RequiresReplace()},
			},
			"notes": schema.StringAttribute{
				MarkdownDescription: "Free-form notes, up to 1024 characters.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(1024)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the entry was created (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *suppressionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *suppressionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan suppressionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	entry := client.Suppression{Recipient: plan.Recipient.ValueString(), Notes: plan.Notes.ValueStringPointer()}
	if !plan.SuppressionTypes.IsUnknown() {
		resp.Diagnostics.Append(plan.SuppressionTypes.ElementsAs(ctx, &entry.SuppressionTypes, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.CreateSuppression(ctx, entry); err != nil {
		resp.Diagnostics.AddError("Creating suppression entry", err.Error())
		return
	}
	created, err := r.client.GetSuppression(ctx, entry.Recipient, client.SuppressionSourceAPI)
	if err != nil || created == nil {
		resp.Diagnostics.AddError("Reading created suppression entry", entry.Recipient+": not found after creation")
		return
	}
	r.setFromAPI(ctx, &plan, created, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *suppressionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state suppressionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	entry, err := r.client.GetSuppression(ctx, state.Recipient.ValueString(), client.SuppressionSourceAPI)
	if err != nil {
		resp.Diagnostics.AddError("Reading suppression entry", err.Error())
		return
	}
	if entry == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	r.setFromAPI(ctx, &state, entry, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *suppressionResource) setFromAPI(ctx context.Context, m *suppressionModel, e *client.Suppression, diags *diag.Diagnostics) {
	m.ID = m.Recipient
	set, d := types.SetValueFrom(ctx, types.StringType, e.SuppressionTypes)
	diags.Append(d...)
	m.SuppressionTypes = set
	if e.Notes != nil && *e.Notes != "" {
		m.Notes = types.StringPointerValue(e.Notes)
	} else if !m.Notes.IsNull() && m.Notes.ValueString() != "" {
		m.Notes = types.StringNull()
	}
	m.CreatedAt = types.StringValue(e.CreatedAt)
}

// Update is never called: every argument requires replacement.
func (r *suppressionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Suppression entries cannot be updated", "Every argument requires replacement.")
}

func (r *suppressionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state suppressionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteSuppression(ctx, state.Recipient.ValueString(), client.SuppressionSourceAPI)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Deleting suppression entry", err.Error())
	}
}

func (r *suppressionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recipient"), req.ID)...)
}
