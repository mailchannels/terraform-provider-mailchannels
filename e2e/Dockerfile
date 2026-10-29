# Image for the end-to-end test: the provider, the Email API stub,
# Terraform, and the tools run.sh uses.
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/terraform-provider-mailchannels . \
 && CGO_ENABLED=0 go build -o /out/stubapi ./cmd/stubapi

FROM debian:bookworm-slim
ARG TERRAFORM_VERSION=1.17.1
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl jq unzip dnsutils \
 && curl -fsSLo /tmp/tf.zip "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" \
 && unzip /tmp/tf.zip -d /usr/local/bin && rm /tmp/tf.zip \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/stubapi /usr/local/bin/stubapi
# The examples require "~> 0.1", so the local build is mirrored as 0.1.0.
COPY --from=build /out/terraform-provider-mailchannels /mirror/registry.terraform.io/mailchannels/mailchannels/0.1.0/linux_amd64/
COPY examples /examples
COPY e2e/run.sh /usr/local/bin/run-e2e
