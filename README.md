# Terraform Provider for MailChannels

Manage [MailChannels Email API](https://docs.mailchannels.net/email-api) resources as code, so each environment's
sending setup is declared and reproducible:

| Type | Name | Create | Read | Update | Delete | Import |
| --- | --- | --- | --- | --- | --- | --- |
| Resource | `mailchannels_dkim_key` | yes | yes | replace | sets status `retired` | `<domain>/<selector>` |
| Resource | `mailchannels_webhook` | yes | yes | replace | yes (see limitations) | endpoint URL |
| Resource | `mailchannels_sub_account` | yes | yes | `enabled` in place; name replaces | yes | handle |
| Resource | `mailchannels_sub_account_limit` | yes | yes | yes | resets to the parent's limit | handle |
| Resource | `mailchannels_suppression` | yes | yes | replace | yes | recipient |
| Data source | `mailchannels_check_domain` | | yes | | | |

`mailchannels_dkim_key` exposes `dns_record_name`, `dns_record_type` and `dns_record_value` (plus
`dns_record_value_chunks` for DNS providers that need TXT values split into 255-character strings), so the DKIM record
can be published with any DNS provider in the same apply.

```terraform
provider "mailchannels" {} # reads MAILCHANNELS_API_KEY

resource "mailchannels_dkim_key" "example" {
  domain   = "example.com"
  selector = "mcdkim"
}

resource "cloudflare_dns_record" "dkim" {
  zone_id = var.cloudflare_zone_id
  name    = mailchannels_dkim_key.example.dns_record_name
  type    = mailchannels_dkim_key.example.dns_record_type
  content = mailchannels_dkim_key.example.dns_record_value
  ttl     = 3600
}
```

[`examples/complete`](examples/complete/main.tf) provisions a domain end to end: a sub-account and its limit, a DKIM
key, the DKIM, SPF and Domain Lockdown records, a webhook and a domain check. The provider documentation is in
[`docs/`](docs/index.md).

## Authentication

Set `MAILCHANNELS_API_KEY`, or `api_key` in the provider block (marked sensitive). `api_url` (or
`MAILCHANNELS_API_URL`) overrides the API base URL, `https://api.mailchannels.net/tx/v1`.

## Limitations of the Email API

The provider uses only the public Email API, which currently lacks:

* **Deleting one webhook.** `DELETE /webhook` removes every endpoint. Destroying one `mailchannels_webhook` deletes
  all of them and registers the others again, so events can be missed for a moment.
* **Deleting a DKIM key.** Destroy sets the key's status to `retired`, and the selector cannot be reused.
* **Reading one sub-account or webhook.** The provider lists them and filters.
* **Renaming a sub-account** or **changing a suppression entry.** These replace the resource.

## Development

Requires Go (see `go.mod`) and Terraform 1.5 or later.

```sh
make build      # compile
make lint       # golangci-lint
make generate   # terraform fmt the examples and regenerate docs/ with tfplugindocs
make testacc    # unit and acceptance tests
make e2e        # the end-to-end behaviour script, in Docker
```

Acceptance tests run against [`internal/stubapi`](internal/stubapi), a stateful in-memory Email API stub. It
validates every request and every response against the public OpenAPI spec
([`internal/stubapi/openapi.yaml`](internal/stubapi/openapi.yaml), copied from transactional-api
`docs/mintlify/api-reference/openapi.yaml`), and the tests fail on any mismatch. No API key or network access to
MailChannels is needed. Refresh the copy when the API changes.

`make e2e` builds the provider and stub into an image and runs [`e2e/run.sh`](e2e/run.sh) against the stub and a
BIND server that accepts dynamic updates: apply `examples/complete`, plan (no changes), change the sub-account limit
(exactly one update request), import an existing webhook (no diff), and destroy (everything deleted).

## Releasing

Releases are built by [GoReleaser](.goreleaser.yml) when a `v*` tag is pushed (see [`ci/github-workflows`](ci/github-workflows/README.md); the workflows still need moving to `.github/workflows/`), and signed with the GPG key in the
`GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets. To publish to the Terraform Registry as
`mailchannels/mailchannels`, add the key's public half to the Registry namespace, sign in with the `mailchannels`
GitHub organization, publish this repository, then push the tag.
