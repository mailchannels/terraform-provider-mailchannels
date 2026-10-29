#!/usr/bin/env bash
# Runs the OVERLORD-34 behaviour script against the Email API stub and a
# BIND server that accepts dynamic updates:
#   1. apply examples/complete: DKIM key, webhook and sub-account created
#   2. plan: no changes
#   3. change the sub-account limit and apply: one update request
#   4. import an existing webhook: in state, no diff
#   5. destroy: everything deleted at the stub
# Every request the stub receives is validated against the OpenAPI spec;
# any validation error fails the run.
set -euo pipefail

STUB_URL=${STUB_URL:-http://127.0.0.1:8080}
DNS_SERVER=${DNS_SERVER:-127.0.0.1}
DNS_PORT=${DNS_PORT:-5300}
OUT=${OUT:-/out}
mkdir -p "$OUT"
exec > >(tee "$OUT/e2e.log") 2>&1

export MAILCHANNELS_API_URL="$STUB_URL/tx/v1"
export MAILCHANNELS_API_KEY="synthetic-e2e-key"
export TF_IN_AUTOMATION=1 TF_INPUT=0 TF_CLI_CONFIG_FILE=/tmp/terraformrc
cat > "$TF_CLI_CONFIG_FILE" <<CONF
provider_installation {
  filesystem_mirror {
    path    = "/mirror"
    include = ["registry.terraform.io/mailchannels/mailchannels"]
  }
  direct {
    exclude = ["registry.terraform.io/mailchannels/mailchannels"]
  }
}
CONF

step() { printf '\n=== %s ===\n' "$*"; }
fail() { echo "FAIL: $*"; exit 1; }
pass() { echo "PASS: $*"; }
state() { curl -fsS "$STUB_URL/__stub/state"; }
requests() { curl -fsS "$STUB_URL/__stub/requests"; }
request_count() { requests | jq '.requests | length'; }
# Mutating API calls since request number $1, ignoring the read-only check-domain call.
writes_since() { requests | jq -c --argjson n "$1" '[.requests[$n:][] | select(.method != "GET" and .path != "/check-domain") | "\(.method) \(.path) \(.status)"]'; }
plan_no_changes() {
  set +e; terraform plan -detailed-exitcode -no-color >"$OUT/plan-$1.txt"; local rc=$?; set -e
  cat "$OUT/plan-$1.txt"
  [ "$rc" -eq 0 ] || fail "$1: terraform plan exit code $rc, want 0 (no changes)"
  pass "$1: terraform plan shows no changes"
}

for _ in $(seq 60); do curl -fsS "$STUB_URL/__stub/state" >/dev/null 2>&1 && break; sleep 1; done
for _ in $(seq 60); do dig +short -p "$DNS_PORT" @"$DNS_SERVER" SOA example.test >/dev/null 2>&1 && break; sleep 1; done
curl -fsS -X POST "$STUB_URL/__stub/reset"

work=$(mktemp -d)
cp /examples/complete/main.tf "$work/"
cd "$work"
cat > terraform.tfvars <<VARS
domain                  = "example.test"
environment             = "staging"
mailchannels_account_id = "synthetic-account"
webhook_endpoint        = "https://hooks.example.test/events"
dns_server              = "$DNS_SERVER"
dns_port                = $DNS_PORT
VARS
terraform init -no-color >/dev/null
terraform version -no-color

step "1. terraform apply examples/complete"
terraform apply -auto-approve -no-color
state | jq . | tee "$OUT/state-after-apply.json"
handle=$(terraform output -raw sub_account_handle)
[ "$(state | jq -r '.dkim_keys["example.test/mcdkim"]')" = active ] || fail "DKIM key not active at the stub"
[ "$(state | jq -r '.webhooks | join(",")')" = "https://hooks.example.test/events" ] || fail "webhook not registered at the stub"
[ "$(state | jq -r --arg h "$handle" '.sub_accounts[$h]')" = 10000 ] || fail "sub-account $handle with limit 10000 missing at the stub"
pass "DKIM key, webhook and sub-account (limit 10000) created at the stub"
want=$(terraform output -json dkim_record | jq -r .value)
got=$(dig +short -p "$DNS_PORT" @"$DNS_SERVER" TXT mcdkim._domainkey.example.test | sed 's/" "//g; s/"//g')
[ "$got" = "$want" ] || fail "DKIM TXT record in DNS is '$got', want '$want'"
pass "DKIM TXT record published through the DNS provider and matches dns_record_value"
dig +short -p "$DNS_PORT" @"$DNS_SERVER" TXT _mailchannels.example.test | grep -q 'v=mc1 auth=synthetic-account' || fail "Domain Lockdown record missing"
dig +short -p "$DNS_PORT" @"$DNS_SERVER" TXT example.test | grep -q 'include:relay.mailchannels.net' || fail "SPF record missing"
pass "SPF and Domain Lockdown records published"

step "2. terraform plan after apply"
plan_no_changes step2

step "3. change the sub-account limit and apply"
mark=$(request_count)
terraform apply -auto-approve -no-color -var monthly_send_limit=20000
writes=$(writes_since "$mark")
echo "mutating requests: $writes"
[ "$writes" = "[\"PUT /sub-account/$handle/limit 200\"]" ] || fail "want exactly one PUT of the limit, got $writes"
[ "$(state | jq -r --arg h "$handle" '.sub_accounts[$h]')" = 20000 ] || fail "limit not updated at the stub"
pass "exactly one update request: PUT /sub-account/$handle/limit"
echo 'monthly_send_limit = 20000' >> terraform.tfvars
plan_no_changes step3

step "4. terraform import an existing stub webhook"
curl -fsS -X POST -H 'X-Api-Key: synthetic-e2e-key' "$MAILCHANNELS_API_URL/webhook?endpoint=https://hooks.example.test/existing"
cat > imported.tf <<'TF'
resource "mailchannels_webhook" "existing" {
  endpoint = "https://hooks.example.test/existing"
}
TF
terraform import -no-color mailchannels_webhook.existing https://hooks.example.test/existing
terraform state show -no-color mailchannels_webhook.existing
plan_no_changes step4

step "5. terraform destroy"
terraform destroy -auto-approve -no-color
state | jq . | tee "$OUT/state-after-destroy.json"
[ "$(state | jq -r '.webhooks | length')" = 0 ] || fail "webhooks left at the stub"
[ "$(state | jq -r '.sub_accounts | length')" = 0 ] || fail "sub-accounts left at the stub"
[ "$(state | jq -r '[.dkim_keys[] | select(. == "active" or . == "rotated")] | length')" = 0 ] || fail "active DKIM keys left at the stub"
[ -z "$(dig +short -p "$DNS_PORT" @"$DNS_SERVER" TXT mcdkim._domainkey.example.test)" ] || fail "DKIM TXT record left in DNS"
pass "all resources deleted at the stub (DKIM key retired: the API has no delete) and DNS records removed"

requests | jq . > "$OUT/stub-requests.json"
errors=$(requests | jq '.validation_errors | length')
[ "$errors" = 0 ] || { requests | jq '.validation_errors'; fail "$errors requests or responses did not match the OpenAPI spec"; }
pass "$(request_count) API requests, all valid against the OpenAPI spec"
echo; echo "E2E PASSED"
