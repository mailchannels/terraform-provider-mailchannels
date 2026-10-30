resource "mailchannels_suppression" "example" {
  recipient         = "do-not-email@example.com"
  suppression_types = ["non-transactional"]
  notes             = "Requested by the recipient"
}
