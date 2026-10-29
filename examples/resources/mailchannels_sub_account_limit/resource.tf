resource "mailchannels_sub_account" "staging" {
  handle       = "examplestaging"
  company_name = "Example Inc. (staging)"
}

resource "mailchannels_sub_account_limit" "staging" {
  handle = mailchannels_sub_account.staging.handle
  sends  = 10000
}
