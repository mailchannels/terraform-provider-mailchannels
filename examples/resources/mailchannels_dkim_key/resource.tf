resource "mailchannels_dkim_key" "example" {
  domain   = "example.com"
  selector = "mcdkim"
}

# Publish the key with any DNS provider, for example Cloudflare:
resource "cloudflare_dns_record" "dkim" {
  zone_id = var.cloudflare_zone_id
  name    = mailchannels_dkim_key.example.dns_record_name
  type    = mailchannels_dkim_key.example.dns_record_type
  content = mailchannels_dkim_key.example.dns_record_value
  ttl     = 3600
}
