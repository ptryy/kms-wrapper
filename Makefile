BINARY := kms-wrapper
PLUGIN_BINARY := kms-vault-plugin
PLUGIN_OUT := vault/plugins/$(PLUGIN_BINARY)
SWAG ?= $(shell go env GOPATH)/bin/swag

.PHONY: build build-plugin test lint swagger swagger-check dev-up dev-down run-gateway scrub-env

build:
	go build -o bin/$(BINARY) ./cmd/kms-wrapper

build-plugin:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(PLUGIN_OUT) ./cmd/kms-vault-plugin

test:
	go test ./...

lint:
	golangci-lint run ./...

swagger:
	$(SWAG) init -g cmd/kms-wrapper/root.go --output docs --outputTypes go,json,yaml --parseInternal --parseDependency --v3.1
	go run ./cmd/swagger-postprocess

swagger-check: swagger
	@git diff --exit-code docs/ || (echo "swagger docs out of date - run make swagger" && exit 1)

dev-up: build-plugin
	docker compose up -d
	./vault/init.sh

dev-down:
	docker compose down

run-gateway:
	go run ./cmd/kms-wrapper serve --config config.yaml

# scrub-env resets the local (gitignored) .env back to the placeholder values
# from .env.example. Useful between dev-up sessions so a live token issued by
# vault/init.sh doesn't accidentally persist across a context switch.
scrub-env:
	@if [ ! -f .env.example ]; then echo "missing .env.example"; exit 1; fi
	@cp .env.example .env
	@echo "scrubbed .env back to .env.example placeholders"
