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
