package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

// dkimDestroyStatus is the status a DKIM key gets on destroy; the API has no
// call that deletes a key.
const dkimDestroyStatus = "retired"

// maxTXTStringLength is the longest character-string a TXT record can hold.
const maxTXTStringLength = 255

var (
	_ resource.Resource                = &dkimKeyResource{}
	_ resource.ResourceWithConfigure   = &dkimKeyResource{}
	_ resource.ResourceWithImportState = &dkimKeyResource{}
)

// NewDKIMKeyResource returns the mailchannels_dkim_key resource.
func NewDKIMKeyResource() resource.Resource { return &dkimKeyResource{} }

type dkimKeyResource struct{ client *client.Client }

type dkimKeyModel struct {
	ID                   types.String `tfsdk:"id"`
	Domain               types.String `tfsdk:"domain"`
	Selector             types.String `tfsdk:"selector"`
	Algorithm            types.String `tfsdk:"algorithm"`
	KeyLength            types.Int64  `tfsdk:"key_length"`
	PublicKey            types.String `tfsdk:"public_key"`
	Status               types.String `tfsdk:"status"`
	CreatedAt            types.String `tfsdk:"created_at"`
	DNSRecordName        types.String `tfsdk:"dns_record_name"`
	DNSRecordType        types.String `tfsdk:"dns_record_type"`
	DNSRecordValue       types.String `tfsdk:"dns_record_value"`
	DNSRecordValueChunks types.List   `tfsdk:"dns_record_value_chunks"`
}

func (r *dkimKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dkim_key"
}

func (r *dkimKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computedString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			MarkdownDescription: desc,
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A MailChannels-managed DKIM key pair for a sending domain. MailChannels keeps the private key " +
			"and signs messages with it; publish `dns_record_value` as a TXT record named `dns_record_name` with your DNS provider.\n\n" +
			"The Email API cannot delete a key or change its selector, algorithm or length: changing any of those replaces the key, " +
			"and destroying the resource sets the key's status to `" + dkimDestroyStatus + "`. A key that is retired or revoked " +
			"outside Terraform is treated as deleted and created again on the next apply, which fails with a conflict because the " +
			"selector is still taken; choose a new selector in that case.",
		Attributes: map[string]schema.Attribute{
			"id": computedString("`<domain>/<selector>`."),
			"domain": schema.StringAttribute{
				MarkdownDescription: "Sending domain the key signs for, in lower case.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"selector": schema.StringAttribute{
				MarkdownDescription: "DKIM selector, 1 to 63 characters.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"algorithm": schema.StringAttribute{
				MarkdownDescription: "Key algorithm. Only `rsa` is supported.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("rsa"),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key_length": schema.Int64Attribute{
				MarkdownDescription: "Key length in bits: 1024, 2048, 3072 or 4096.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(2048),
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"public_key":       computedString("Base64-encoded public key."),
			"status":           computedString("Key status: `active`, `rotated`, `retired` or `revoked`."),
			"created_at":       computedString("When the key was created (RFC 3339)."),
			"dns_record_name":  computedString("Fully qualified name of the DKIM TXT record, `<selector>._domainkey.<domain>`, without a trailing dot."),
			"dns_record_type":  computedString("Type of the DKIM DNS record, `TXT`."),
			"dns_record_value": computedString("Value of the DKIM TXT record, `v=DKIM1; k=rsa; p=...`."),
			"dns_record_value_chunks": schema.ListAttribute{
				MarkdownDescription: "`dns_record_value` split into strings of at most 255 characters, for DNS providers " +
					"that need a long TXT value as several strings. For Route 53, use " +
					"`records = [join(\"\\\"\\\"\", mailchannels_dkim_key.example.dns_record_value_chunks)]`.",
				ElementType:   types.StringType,
				Computed:      true,
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *dkimKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *dkimKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dkimKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, selector := plan.Domain.ValueString(), plan.Selector.ValueString()
	_, err := r.client.CreateDKIMKey(ctx, domain, client.CreateDKIMKeyRequest{
		Selector:  selector,
		Algorithm: plan.Algorithm.ValueString(),
		KeyLength: plan.KeyLength.ValueInt64(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Creating DKIM key", err.Error())
		return
	}
	// The create response has no DNS record; read the key back for it.
	key, err := r.client.GetDKIMKey(ctx, domain, selector)
	if err != nil || key == nil {
		resp.Diagnostics.AddError("Reading created DKIM key", fmt.Sprintf("key %s/%s: %v", domain, selector, err))
		return
	}
	resp.Diagnostics.Append(setDKIMKey(ctx, &plan, key)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dkimKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dkimKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.GetDKIMKey(ctx, state.Domain.ValueString(), state.Selector.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading DKIM key", err.Error())
		return
	}
	if key == nil || key.Status == "retired" || key.Status == "revoked" {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(setDKIMKey(ctx, &state, key)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every configurable attribute requires replacement.
func (r *dkimKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("DKIM keys cannot be updated", "Every DKIM key attribute requires replacement.")
}

func (r *dkimKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dkimKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.UpdateDKIMKeyStatus(ctx, state.Domain.ValueString(), state.Selector.ValueString(), dkimDestroyStatus)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Retiring DKIM key", err.Error())
	}
}

func (r *dkimKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	domain, selector, ok := strings.Cut(req.ID, "/")
	if !ok || domain == "" || selector == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("expected <domain>/<selector>, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("domain"), domain)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("selector"), selector)...)
}

func setDKIMKey(ctx context.Context, m *dkimKeyModel, key *client.DKIMKey) diag.Diagnostics {
	// Keep domain and selector as configured: the API may normalise them.
	m.ID = types.StringValue(m.Domain.ValueString() + "/" + m.Selector.ValueString())
	m.Algorithm = types.StringValue(key.Algorithm)
	if key.KeyLength != 0 {
		m.KeyLength = types.Int64Value(key.KeyLength)
	}
	m.PublicKey = types.StringValue(key.PublicKey)
	m.Status = types.StringValue(key.Status)
	m.CreatedAt = types.StringPointerValue(key.CreatedAt)

	name := key.Selector + "._domainkey." + key.Domain
	recordType := "TXT"
	value := fmt.Sprintf("v=DKIM1; k=%s; p=%s", key.Algorithm, key.PublicKey)
	if len(key.DNSRecords) > 0 {
		name, recordType, value = key.DNSRecords[0].Name, key.DNSRecords[0].Type, key.DNSRecords[0].Value
	}
	m.DNSRecordName = types.StringValue(strings.TrimSuffix(name, "."))
	m.DNSRecordType = types.StringValue(recordType)
	m.DNSRecordValue = types.StringValue(value)
	chunks, d := types.ListValueFrom(ctx, types.StringType, chunk(value, maxTXTStringLength))
	m.DNSRecordValueChunks = chunks
	return d
}

func chunk(s string, size int) []string {
	out := []string{}
	for len(s) > size {
		out = append(out, s[:size])
		s = s[size:]
	}
	return append(out, s)
}
