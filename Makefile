BINARY := kms-wrapper
PLUGIN_BINARY := kms-vault-plugin
PLUGIN_OUT := vault/plugins/$(PLUGIN_BINARY)

.PHONY: build build-plugin test lint dev-up dev-down run-gateway

build:
	go build -o bin/$(BINARY) ./cmd/kms-wrapper

build-plugin:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o $(PLUGIN_OUT) ./cmd/kms-vault-plugin

test:
	go test ./...

lint:
	golangci-lint run ./...

dev-up: build-plugin
	docker compose up -d
	./vault/init.sh

dev-down:
	docker compose down

run-gateway:
	go run ./cmd/kms-wrapper serve
