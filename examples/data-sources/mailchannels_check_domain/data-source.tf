data "mailchannels_check_domain" "example" {
  domain = "example.com"
}

output "mailchannels_ready" {
  value = data.mailchannels_check_domain.example.all_passed
}
