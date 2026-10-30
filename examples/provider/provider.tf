terraform {
  required_providers {
    mailchannels = {
      source  = "mailchannels/mailchannels"
      version = "~> 0.1"
    }
  }
}

# Reads the API key from MAILCHANNELS_API_KEY when api_key is not set.
provider "mailchannels" {}
