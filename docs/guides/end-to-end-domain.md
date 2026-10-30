---
page_title: "Provision a sending domain end to end"
subcategory: ""
description: |-
  Create a sub-account, DKIM key, DNS records and webhook for one environment, and check the domain.
---

# Provision a sending domain end to end

This configuration sets up one environment (for example staging) with a single `terraform apply`:

* a sub-account for the environment, with a monthly send limit;
* a MailChannels-managed DKIM key;
* the DKIM, SPF and [Domain Lockdown](https://support.mailchannels.com/hc/en-us/articles/16918954360845) TXT
  records, published with the [hashicorp/dns](https://registry.terraform.io/providers/hashicorp/dns/latest/docs)
  provider through RFC 2136 dynamic updates;
* a webhook for delivery events;
* a check that warns until MailChannels sees the DNS records.

Apply it once per environment, with a workspace or a separate state per environment.

```terraform
# Provisions a sending domain for one environment end to end:
# a sub-account with a monthly limit, a DKIM key, the SPF, DKIM and
# Domain Lockdown DNS records (through RFC 2136 dynamic DNS updates with
# the hashicorp/dns provider), a webhook, and a domain check.
#
#   export MAILCHANNELS_API_KEY=...   # a parent account's key
#   terraform apply -var domain=example.com -var dns_server=ns1.example.com \
#     -var mailchannels_account_id=... -var webhook_endpoint=https://...
#
# examples/cloudflare shows the same DNS records with Cloudflare.

terraform {
  required_version = ">= 1.5"
  required_providers {
    mailchannels = {
      source  = "mailchannels/mailchannels"
      version = "~> 0.1"
    }
    dns = {
      source  = "hashicorp/dns"
      version = "~> 3.4"
    }
  }
}

variable "domain" {
  description = "Sending domain; also the DNS zone the records are added to."
  type        = string
}

variable "environment" {
  description = "Environment name, used for the sub-account handle and name."
  type        = string
  default     = "staging"
}

variable "mailchannels_account_id" {
  description = "MailChannels account ID allowed to send for the domain (Domain Lockdown)."
  type        = string
}

variable "monthly_send_limit" {
  description = "Messages the environment's sub-account may send per month."
  type        = number
  default     = 10000
}

variable "dkim_selector" {
  type    = string
  default = "mcdkim"
}

variable "webhook_endpoint" {
  description = "HTTPS URL that receives delivery events."
  type        = string
}

variable "dns_server" {
  description = "DNS server that accepts RFC 2136 updates for the zone."
  type        = string
}

variable "dns_port" {
  type    = number
  default = 53
}

provider "mailchannels" {}

# TSIG credentials, if the server needs them, come from the DNS_UPDATE_KEYNAME,
# DNS_UPDATE_KEYALGORITHM and DNS_UPDATE_KEYSECRET environment variables.
provider "dns" {
  update {
    server = var.dns_server
    port   = var.dns_port
  }
}

locals {
  zone = "${trimsuffix(var.domain, ".")}."
}

resource "mailchannels_sub_account" "env" {
  handle       = "${replace(lower(var.environment), "/[^a-z0-9]/", "")}${substr(sha1(var.domain), 0, 8)}"
  company_name = "${var.domain} (${var.environment})"
}

resource "mailchannels_sub_account_limit" "env" {
  handle = mailchannels_sub_account.env.handle
  sends  = var.monthly_send_limit
}

resource "mailchannels_dkim_key" "domain" {
  domain   = var.domain
  selector = var.dkim_selector
}

resource "dns_txt_record_set" "dkim" {
  zone = local.zone
  name = trimsuffix(mailchannels_dkim_key.domain.dns_record_name, ".${var.domain}")
  txt  = [mailchannels_dkim_key.domain.dns_record_value]
  ttl  = 3600
}

resource "dns_txt_record_set" "spf" {
  zone = local.zone
  txt  = ["v=spf1 include:relay.mailchannels.net ~all"]
  ttl  = 3600
}

resource "dns_txt_record_set" "domain_lockdown" {
  zone = local.zone
  name = "_mailchannels"
  txt  = ["v=mc1 auth=${var.mailchannels_account_id}"]
  ttl  = 3600
}

resource "mailchannels_webhook" "events" {
  endpoint = var.webhook_endpoint
}

# Checks the domain once its DNS records exist. It is a top-level data
# source, not one inside the check block, because Terraform reads a check
# block's own data source on every apply, so the plan would never be empty.
data "mailchannels_check_domain" "domain" {
  domain = mailchannels_dkim_key.domain.domain
  dkim_settings = [{
    dkim_domain   = mailchannels_dkim_key.domain.domain
    dkim_selector = mailchannels_dkim_key.domain.selector
  }]
  depends_on = [dns_txt_record_set.dkim, dns_txt_record_set.spf, dns_txt_record_set.domain_lockdown]
}

# Warns, without failing the apply, until DNS has propagated.
check "domain_ready" {
  assert {
    condition     = data.mailchannels_check_domain.domain.all_passed
    error_message = "MailChannels domain check has not passed yet: ${join(", ", data.mailchannels_check_domain.domain.references)}"
  }
}

output "sub_account_handle" {
  value = mailchannels_sub_account.env.handle
}

output "dkim_record" {
  value = {
    name  = mailchannels_dkim_key.domain.dns_record_name
    type  = mailchannels_dkim_key.domain.dns_record_type
    value = mailchannels_dkim_key.domain.dns_record_value
  }
}
```

## With Cloudflare

To publish the records with Cloudflare instead, use `cloudflare_dns_record`:

```terraform
# The DNS records of examples/complete, published with Cloudflare.
#
#   export MAILCHANNELS_API_KEY=... CLOUDFLARE_API_TOKEN=...

terraform {
  required_version = ">= 1.5"
  required_providers {
    mailchannels = {
      source  = "mailchannels/mailchannels"
      version = "~> 0.1"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }
}

variable "domain" {
  type = string
}

variable "cloudflare_zone_id" {
  type = string
}

variable "mailchannels_account_id" {
  type = string
}

provider "mailchannels" {}
provider "cloudflare" {}

resource "mailchannels_dkim_key" "domain" {
  domain   = var.domain
  selector = "mcdkim"
}

resource "cloudflare_dns_record" "dkim" {
  zone_id = var.cloudflare_zone_id
  name    = mailchannels_dkim_key.domain.dns_record_name
  type    = mailchannels_dkim_key.domain.dns_record_type
  content = mailchannels_dkim_key.domain.dns_record_value
  ttl     = 3600
}

resource "cloudflare_dns_record" "spf" {
  zone_id = var.cloudflare_zone_id
  name    = var.domain
  type    = "TXT"
  content = "v=spf1 include:relay.mailchannels.net ~all"
  ttl     = 3600
}

resource "cloudflare_dns_record" "domain_lockdown" {
  zone_id = var.cloudflare_zone_id
  name    = "_mailchannels.${var.domain}"
  type    = "TXT"
  content = "v=mc1 auth=${var.mailchannels_account_id}"
  ttl     = 3600
}
```

## With Route 53

Route 53 needs TXT values longer than 255 characters split into quoted strings. Use `dns_record_value_chunks`:

```terraform
resource "aws_route53_record" "dkim" {
  zone_id = var.route53_zone_id
  name    = mailchannels_dkim_key.domain.dns_record_name
  type    = "TXT"
  ttl     = 3600
  records = [join("\"\"", mailchannels_dkim_key.domain.dns_record_value_chunks)]
}
```
