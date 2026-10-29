default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

# Formats the examples and regenerates docs/ with tfplugindocs.
generate:
	go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

# Acceptance tests run against the in-memory Email API stub, not the real API.
testacc:
	TF_ACC=1 go test -v -cover -timeout 30m ./...

# Runs the behaviour script (apply, plan, update, import, destroy) in Docker.
e2e:
	docker build -f e2e/Dockerfile -t terraform-provider-mailchannels-e2e .
	docker compose -f e2e/docker-compose.yml up --no-build --abort-on-container-exit --exit-code-from runner
	docker compose -f e2e/docker-compose.yml down

.PHONY: fmt lint test testacc build install generate e2e
