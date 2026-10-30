package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mailchannels/terraform-provider-mailchannels/internal/client"
)

var (
	_ datasource.DataSource              = &checkDomainDataSource{}
	_ datasource.DataSourceWithConfigure = &checkDomainDataSource{}
)

// NewCheckDomainDataSource returns the mailchannels_check_domain data source.
func NewCheckDomainDataSource() datasource.DataSource { return &checkDomainDataSource{} }

type checkDomainDataSource struct{ client *client.Client }

type checkDomainDKIMModel struct {
	Domain    types.String `tfsdk:"domain"`
	Selector  types.String `tfsdk:"selector"`
	KeyStatus types.String `tfsdk:"key_status"`
	Verdict   types.String `tfsdk:"verdict"`
	Reason    types.String `tfsdk:"reason"`
}

type checkDomainSettingModel struct {
	Domain   types.String `tfsdk:"dkim_domain"`
	Selector types.String `tfsdk:"dkim_selector"`
}

type checkDomainModel struct {
	ID                    types.String              `tfsdk:"id"`
	Domain                types.String              `tfsdk:"domain"`
	EnvelopeFromDomain    types.String              `tfsdk:"envelope_from_domain"`
	SenderID              types.String              `tfsdk:"sender_id"`
	DKIMSettings          []checkDomainSettingModel `tfsdk:"dkim_settings"`
	AllPassed             types.Bool                `tfsdk:"all_passed"`
	DKIM                  []checkDomainDKIMModel    `tfsdk:"dkim"`
	SPFVerdict            types.String              `tfsdk:"spf_verdict"`
	SPFReason             types.String              `tfsdk:"spf_reason"`
	SPFRecord             types.String              `tfsdk:"spf_record"`
	DomainLockdownVerdict types.String              `tfsdk:"domain_lockdown_verdict"`
	DomainLockdownReason  types.String              `tfsdk:"domain_lockdown_reason"`
	SenderDomainVerdict   types.String              `tfsdk:"sender_domain_verdict"`
	References            []types.String            `tfsdk:"references"`
}

func (d *checkDomainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_check_domain"
}

func (d *checkDomainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Checks that a domain's SPF, DKIM, Domain Lockdown and sender-domain DNS records let it " +
			"send through MailChannels. Use it in a `check` block or a postcondition to verify DNS after provisioning.",
		Attributes: map[string]schema.Attribute{
			"id": str("The domain."),
			"domain": schema.StringAttribute{
				MarkdownDescription: "Sending domain to check.",
				Required:            true,
			},
			"envelope_from_domain": schema.StringAttribute{
				MarkdownDescription: "Envelope-from domain, when it differs from `domain`; SPF and Domain Lockdown are checked against it.",
				Optional:            true,
			},
			"sender_id": schema.StringAttribute{
				MarkdownDescription: "Sender ID used for Domain Lockdown, as sent in `X-MailChannels-SenderId`.",
				Optional:            true,
			},
			"dkim_settings": schema.ListNestedAttribute{
				MarkdownDescription: "MailChannels-managed DKIM keys to check, at most 10. Defaults to every stored key for `domain`.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"dkim_domain":   schema.StringAttribute{MarkdownDescription: "Signing domain (`d=`). Defaults to `domain`.", Optional: true},
						"dkim_selector": schema.StringAttribute{MarkdownDescription: "Selector of a stored key. All stored keys when omitted.", Optional: true},
					},
				},
			},
			"all_passed": schema.BoolAttribute{
				MarkdownDescription: "True when SPF, every DKIM check, Domain Lockdown and the sender-domain check passed.",
				Computed:            true,
			},
			"dkim": schema.ListNestedAttribute{
				MarkdownDescription: "One result per DKIM key checked.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"domain":     str("Signing domain."),
						"selector":   str("Selector."),
						"key_status": str("Status of the MailChannels-managed key."),
						"verdict":    str("`passed` or `failed`."),
						"reason":     str("Explanation."),
					},
				},
			},
			"spf_verdict":             str("SPF verdict, such as `passed`, `failed` or `soft failed`."),
			"spf_reason":              str("Explanation of the SPF verdict."),
			"spf_record":              str("SPF record that was checked."),
			"domain_lockdown_verdict": str("Domain Lockdown verdict, `passed` or `failed`."),
			"domain_lockdown_reason":  str("Explanation of the Domain Lockdown verdict."),
			"sender_domain_verdict":   str("Whether the domain has the A or MX record MailChannels needs, `passed` or `failed`."),
			"references": schema.ListAttribute{
				MarkdownDescription: "Links to documentation for any check that did not pass.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *checkDomainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *checkDomainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m checkDomainModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := client.CheckDomainRequest{
		Domain:             m.Domain.ValueString(),
		EnvelopeFromDomain: m.EnvelopeFromDomain.ValueString(),
		SenderID:           m.SenderID.ValueString(),
	}
	for _, s := range m.DKIMSettings {
		in.DKIMSettings = append(in.DKIMSettings, client.DKIMSetting{Domain: s.Domain.ValueString(), Selector: s.Selector.ValueString()})
	}
	out, err := d.client.CheckDomain(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Checking domain", err.Error())
		return
	}
	r := out.CheckResults
	allPassed := r.SPF.Verdict == "passed" && r.DomainLockdown.Verdict == "passed" && r.SenderDomain.Verdict == "passed" && len(r.DKIM) > 0
	m.DKIM = []checkDomainDKIMModel{}
	for _, k := range r.DKIM {
		m.DKIM = append(m.DKIM, checkDomainDKIMModel{
			Domain: types.StringValue(k.Domain), Selector: types.StringValue(k.Selector), KeyStatus: types.StringValue(k.KeyStatus),
			Verdict: types.StringValue(k.Verdict), Reason: types.StringValue(k.Reason),
		})
		allPassed = allPassed && k.Verdict == "passed"
	}
	m.ID = m.Domain
	m.AllPassed = types.BoolValue(allPassed)
	m.SPFVerdict = types.StringValue(r.SPF.Verdict)
	m.SPFReason = types.StringValue(r.SPF.Reason)
	m.SPFRecord = types.StringValue(r.SPF.Record)
	m.DomainLockdownVerdict = types.StringValue(r.DomainLockdown.Verdict)
	m.DomainLockdownReason = types.StringValue(r.DomainLockdown.Reason)
	m.SenderDomainVerdict = types.StringValue(r.SenderDomain.Verdict)
	m.References = []types.String{}
	for _, ref := range out.References {
		m.References = append(m.References, types.StringValue(ref))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
